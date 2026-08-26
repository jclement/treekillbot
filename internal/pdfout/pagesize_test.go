package pdfout

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
)

func TestNamedPageSizes(t *testing.T) {
	tests := []struct {
		query    string
		wantName string
		wantW    float64 // points
		wantH    float64
	}{
		{"letter", "letter", 612, 792},
		{"Letter", "letter", 612, 792},
		{"  LEGAL ", "legal", 612, 1008},
		{"tabloid", "tabloid", 792, 1224},
		// The A series comes from millimetres, so A4 is 595.25pt rather than
		// the 595.28 a table of rounded points would give. See the file
		// comment: a whole number of ticks is worth 0.009mm. The series stays
		// self-consistent — A4's height is A3's width, A5's height is A4's
		// width — which is what a folded-signature layout depends on.
		{"a3", "a3", 841.875, 1190.5625},
		{"a4", "a4", 595.25, 841.875},
		{"a5", "a5", 419.5, 595.25},
		{"a6", "a6", 297.625, 419.5},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			size, ok := NamedPageSize(tc.query)
			if !ok {
				t.Fatalf("NamedPageSize(%q) not found", tc.query)
			}
			if size.Name != tc.wantName {
				t.Errorf("name = %q, want %q", size.Name, tc.wantName)
			}
			if size.W.Points() != tc.wantW || size.H.Points() != tc.wantH {
				t.Errorf("size = %vx%vpt, want %vx%vpt", size.W.Points(), size.H.Points(), tc.wantW, tc.wantH)
			}
			if !size.IsValid() {
				t.Error("IsValid() = false")
			}
		})
	}

	if _, ok := NamedPageSize("foolscap"); ok {
		t.Error("NamedPageSize accepted an unknown name")
	}
}

// The listing feeds an "unknown page size, try one of ..." diagnostic, so it
// has to be sorted rather than in map order.
func TestPageSizeNamesAreSorted(t *testing.T) {
	want := []string{"a3", "a4", "a5", "a6", "legal", "letter", "tabloid"}
	got := PageSizeNames()
	if len(got) != len(want) {
		t.Fatalf("PageSizeNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PageSizeNames() = %v, want %v", got, want)
		}
	}
}

// Both orientation calls are idempotent, so a document can ask for landscape
// without knowing which way round the size was written.
func TestOrientationIsIdempotent(t *testing.T) {
	portrait, _ := NamedPageSize("a4")
	landscape := portrait.Landscape()

	if landscape.W <= landscape.H {
		t.Errorf("Landscape() = %vx%v, want the long edge horizontal", landscape.W, landscape.H)
	}
	if again := landscape.Landscape(); again != landscape {
		t.Error("Landscape() is not idempotent")
	}
	if back := landscape.Portrait(); back != portrait {
		t.Errorf("Portrait() of a landscape A4 = %+v, want %+v", back, portrait)
	}
	if again := portrait.Portrait(); again != portrait {
		t.Error("Portrait() is not idempotent")
	}
	if landscape.Name != portrait.Name {
		t.Error("rotating lost the size's name")
	}
}

func TestCustomPageSize(t *testing.T) {
	size := CustomPageSize(geom.In(5.5), geom.In(8.5))
	if size.Name != "" {
		t.Errorf("custom size has name %q, want empty", size.Name)
	}
	if size.W.Points() != 396 || size.H.Points() != 612 {
		t.Errorf("size = %vx%v, want 396x612pt", size.W.Points(), size.H.Points())
	}
	if !size.IsValid() {
		t.Error("IsValid() = false")
	}
	if CustomPageSize(0, geom.In(1)).IsValid() {
		t.Error("a zero-width page reports itself valid")
	}
}
