// Package editor serves the side-by-side editing and preview page behind
// `treekillbot edit`.
//
// The preview is faithful rather than approximate because it is produced by the
// same painting code as the PDF, over the same computed rectangles, differing
// only in which render.Canvas receives the operations. There is no second
// layout engine here and no CSS reinterpretation of the document — if the two
// ever disagree, that is a bug in one Canvas, not a drift between two designs.
//
// The server binds to loopback and requires a per-session token on every
// request. That is not theatre: a page on the open internet can reach
// 127.0.0.1, and without the token any site the user happens to have open could
// read and overwrite the file being edited.
package editor

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/svgout"
)

// whitePaper is what the preview paints behind the page. A document usually
// sets no page background, which in print means "whatever the paper is"; on
// screen that would show the browser's own background and make a dark-mode
// preview of a light-mode document look nothing like the print.
func whitePaper() paint.Color { return paint.White }

//go:embed assets/*
var assets embed.FS

// Options configure an editing session.
type Options struct {
	// Path is the document being edited. It is the only file the server will
	// ever read or write.
	Path string
	// Addr is the listen address; an empty port lets the OS choose one.
	Addr string
	// Build carries the same options a command-line build would use, so the
	// preview honours --date, --theme and the rest.
	Build pipeline.Options
}

// Server is a running editing session.
type Server struct {
	options Options
	token   string
	page    *template.Template

	mu      sync.Mutex
	current string // the last source the browser sent, for save-without-render
}

// New prepares a server for a document.
func New(options Options) (*Server, error) {
	page, err := template.ParseFS(assets, "assets/index.html")
	if err != nil {
		return nil, fmt.Errorf("loading the editor page: %w", err)
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	return &Server{options: options, token: token, page: page}, nil
}

// newToken returns a random session token.
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a session token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Listen binds the socket and returns the URL to open, without serving yet, so
// the caller can print the URL before blocking.
func (s *Server) Listen() (net.Listener, string, error) {
	addr := s.options.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("listening on %s: %w", addr, err)
	}
	url := fmt.Sprintf("http://%s/?t=%s", listener.Addr().String(), s.token)
	return listener, url, nil
}

// Serve runs until the listener closes.
func (s *Server) Serve(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/render", s.guard(s.handleRender))
	mux.HandleFunc("/api/save", s.guard(s.handleSave))
	mux.HandleFunc("/api/fonts.css", s.guard(s.handleFonts))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server.Serve(listener)
}

// guard rejects requests without the session token, and requests whose Origin
// is not this server.
//
// The Origin check is the one that stops DNS rebinding: an attacker who tricks
// a browser into resolving their hostname to 127.0.0.1 still cannot forge an
// Origin, so the request is refused even though it reached us.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Treekillbot-Token")
		if token == "" {
			token = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			http.Error(w, "bad or missing session token", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin, r.Host) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) originAllowed(origin, host string) bool {
	return strings.HasSuffix(origin, "//"+host)
}

// handlePage serves the editor itself.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(s.token)) != 1 {
		http.Error(w, "bad or missing session token", http.StatusForbidden)
		return
	}

	source, err := os.ReadFile(s.options.Path)
	if err != nil {
		http.Error(w, "reading the document: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.current = string(source)
	s.mu.Unlock()

	data := struct {
		Title  string
		Path   string
		Token  string
		Source string
	}{
		Title:  s.options.Path,
		Path:   s.options.Path,
		Token:  s.token,
		Source: string(source),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page is the only thing that carries the token, so it must never be
	// cached where another process could read it back.
	w.Header().Set("Cache-Control", "no-store")
	if err := s.page.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderResponse is what the browser gets back for a preview.
type renderResponse struct {
	SVG          string           `json:"svg"`
	Width        float64          `json:"width"`
	Height       float64          `json:"height"`
	Diagnostics  []jsonDiagnostic `json:"diagnostics"`
	Pages        int              `json:"pages"`
	Milliseconds float64          `json:"ms"`
	OK           bool             `json:"ok"`
}

type jsonDiagnostic struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	EndLine  int    `json:"endLine"`
	EndCol   int    `json:"endColumn"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Help     string `json:"help,omitempty"`
	Label    string `json:"label,omitempty"`
}

// handleRender compiles the posted source and returns SVG plus diagnostics.
//
// It renders through StageLayout rather than StageRender: the preview needs
// geometry, not a PDF, and skipping the writer keeps a keystroke's round trip
// in single-digit milliseconds.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.current = body
	s.mu.Unlock()

	started := time.Now()
	source := pulp.NewSource(s.options.Path, body)
	result, err := pipeline.Build(source, pipeline.StageLayout, s.options.Build)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := renderResponse{
		Diagnostics: convertDiagnostics(result.Diags),
		Pages:       1,
		OK:          !result.Diags.HasErrors(),
	}

	// A document with errors still gets whatever geometry survived, so the
	// preview does not blank out while a line is half-typed.
	if result.Document != nil {
		canvas := svgout.New(result.PageSize.Width, result.PageSize.Height, svgout.Options{
			Background: whitePaper(),
		})
		pipeline.RenderTo(result, canvas, s.options.Build)
		response.SVG = canvas.String()
		response.Width = result.PageSize.Width.Points()
		response.Height = result.PageSize.Height.Points()
	}
	response.Milliseconds = float64(time.Since(started).Microseconds()) / 1000

	writeJSON(w, response)
}

// handleSave writes the posted source back to the file.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Preserve the file's existing mode rather than forcing 0644, so an
	// executable or group-writable document stays as the user set it up.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(s.options.Path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(s.options.Path, []byte(body), mode); err != nil {
		http.Error(w, "writing the document: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.current = body
	s.mu.Unlock()
	writeJSON(w, map[string]any{"saved": true, "path": s.options.Path})
}

// handleFonts serves the @font-face rules for every embedded family, so the
// preview uses the same faces the PDF embeds.
//
// They are served separately from the SVG and cached hard: the payload is
// megabytes, and re-sending it on every keystroke would make typing feel
// broken.
func (s *Server) handleFonts(w http.ResponseWriter, r *http.Request) {
	registry := fonts.NewRegistry()
	var b strings.Builder
	for _, family := range registry.Available() {
		for _, style := range family.Styles {
			face, _, err := registry.Resolve(family.Name, style.Style)
			if err != nil || face == nil {
				continue
			}
			b.WriteString(svgout.FontFaceRule(face))
		}
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=86400")
	fmt.Fprint(w, b.String())
}

func readBody(r *http.Request) (string, error) {
	if r.Method != http.MethodPost {
		return "", fmt.Errorf("expected POST")
	}
	defer r.Body.Close()
	const maxDocument = 8 << 20
	var payload struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxDocument)).Decode(&payload); err != nil {
		return "", fmt.Errorf("reading the request: %w", err)
	}
	return payload.Source, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func convertDiagnostics(diags pulp.Diagnostics) []jsonDiagnostic {
	out := make([]jsonDiagnostic, 0, len(diags))
	for _, d := range diags {
		entry := jsonDiagnostic{
			Severity: d.Severity.String(),
			Code:     d.Code,
			Message:  d.Message,
			Help:     d.Help,
			Label:    d.Label,
		}
		if d.Source != nil {
			start := d.Position()
			end := d.Source.Position(d.Span.End)
			entry.Line, entry.Column = start.Line, start.Column
			entry.EndLine, entry.EndCol = end.Line, end.Column
		}
		out = append(out, entry)
	}
	return out
}
