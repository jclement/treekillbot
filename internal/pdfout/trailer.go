// Trailer fields gopdf does not write, added after the fact.
//
// DESIGN.md section 4 requires a /ID derived from a hash rather than from
// randomness, and a fixable /ModDate. gopdf writes its trailer itself and
// exposes no hook for either: /ID appears only when the document is encrypted,
// and PdfInfo has a CreationDate field and no ModDate. Rather than fork the
// library or drop the requirement, the finished bytes are patched.
//
// That is safe, and specifically it is safe here rather than in general,
// because of the order gopdf writes in: the cross-reference table comes first,
// "startxref" points at the table's own offset, and the trailer dictionary
// comes last. Nothing in the file records an offset into the trailer, so
// inserting entries into it moves nothing that anything else refers to.
//
// If gopdf ever changes the shape of what it writes, the patch fails loudly
// instead of producing a subtly broken file — determinism is load-bearing here
// and silently losing it is the worst outcome.
package pdfout

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The literal fragments gopdf's writer emits around the trailer dictionary.
const (
	trailerOpen  = "\ntrailer\n<<\n"
	trailerClose = ">>\nstartxref\n"
	infoOpen     = "/Info <<\n"
	infoClose    = " >>\n"
)

// pdfDateLayout is PDF's date format, matching gopdf's own so that
// /CreationDate and the /ModDate added here are written identically.
const pdfDateLayout = "20060102150405-07'00'"

// idBytes is how many bytes of the digest go into each /ID string. The PDF
// specification calls for a 16-byte file identifier.
const idBytes = 16

var errTrailerShape = errors.New("pdfout: gopdf's trailer is not the shape this build expects; /ID and /ModDate cannot be written deterministically")

// finalizeTrailer adds /ModDate to the information dictionary and /ID to the
// trailer, returning the completed document.
//
// /ID is SHA-256 of everything before the trailer, truncated to 16 bytes and
// written twice, since the original and current identifiers of a file that has
// never been incrementally updated are the same. A hash means two runs over
// the same document produce the same identifier, which a random one — the
// usual implementation — would not.
func finalizeTrailer(doc []byte, modified time.Time) ([]byte, error) {
	trailerAt := bytes.LastIndex(doc, []byte(trailerOpen))
	closeAt := bytes.LastIndex(doc, []byte(trailerClose))
	if trailerAt < 0 || closeAt <= trailerAt {
		return nil, errTrailerShape
	}
	dictStart := trailerAt + len(trailerOpen)
	dict := string(doc[dictStart:closeAt])

	dict, err := insertModDate(dict, modified)
	if err != nil {
		return nil, err
	}

	// The digest covers the objects and the cross-reference table: everything
	// that describes the document's content, and nothing that this function
	// is about to write.
	digest := sha256.Sum256(doc[:trailerAt])
	id := strings.ToUpper(hex.EncodeToString(digest[:idBytes]))
	dict += fmt.Sprintf("/ID [<%s> <%s>]\n", id, id)

	out := make([]byte, 0, len(doc)+len(dict))
	out = append(out, doc[:dictStart]...)
	out = append(out, dict...)
	out = append(out, doc[closeAt:]...)
	return out, nil
}

// insertModDate places /ModDate inside the information dictionary, which is
// written inline in the trailer rather than as an indirect object — the reason
// this can be done with a string splice at all.
func insertModDate(dict string, modified time.Time) (string, error) {
	if !strings.Contains(dict, infoOpen) {
		return "", errTrailerShape
	}
	// The trailer dictionary's own ">>" lives outside this substring, so the
	// last " >>" in it can only be the information dictionary's.
	end := strings.LastIndex(dict, infoClose)
	if end < 0 {
		return "", errTrailerShape
	}
	entry := fmt.Sprintf("/ModDate(D:%s)\n", modified.Format(pdfDateLayout))
	return dict[:end] + entry + dict[end:], nil
}
