package pulp

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
)

func parseOne(t *testing.T, text string) (Value, Diagnostics) {
	t.Helper()
	src := NewSource("t.pulp", text)
	var diags Diagnostics
	vs := ParseValues(src, Span{Start: 0, End: len(text)}, text, &diags)
	if len(vs) == 0 {
		return Value{}, diags
	}
	return vs[0], diags
}

func TestParseLengths(t *testing.T) {
	tests := []struct {
		text string
		want geom.Tick
	}{
		{"16pt", geom.Pt(16)},
		{"0.5in", geom.In(0.5)},
		{"12mm", geom.Mm(12)},
		{"1cm", geom.Cm(1)},
		{"6pc", geom.Pc(6)},
		{"96px", geom.In(1)},
		{"-4pt", geom.Pt(-4)},
		{".5in", geom.In(0.5)},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			v, diags := parseOne(t, tt.text)
			if diags.HasErrors() {
				t.Fatalf("errors: %v", diags)
			}
			if v.Kind != KindLength {
				t.Fatalf("kind = %v, want length", v.Kind)
			}
			if v.Length != tt.want {
				t.Fatalf("length = %d ticks (%.4fpt), want %d", v.Length, v.Length.Points(), tt.want)
			}
		})
	}
}

// em and ex depend on a font size the cascade has not chosen yet, so they must
// survive parsing unresolved rather than silently becoming zero.
func TestRelativeUnitsAreDeferred(t *testing.T) {
	for _, unit := range []string{"em", "ex"} {
		v, diags := parseOne(t, "1.5"+unit)
		if diags.HasErrors() {
			t.Fatalf("errors: %v", diags)
		}
		if v.Kind != KindLength || !v.Relative {
			t.Fatalf("%s: kind=%v relative=%v, want a deferred length", unit, v.Kind, v.Relative)
		}
		if v.Num != 1.5 {
			t.Fatalf("%s: magnitude = %g, want 1.5", unit, v.Num)
		}
	}
}

func TestParseSizes(t *testing.T) {
	tests := []struct {
		text   string
		kind   ValueKind
		pct    int32
		weight int32
	}{
		{"30%", KindPercent, 3000, 0},
		{"100%", KindPercent, 10000, 0},
		{"12.5%", KindPercent, 1250, 0},
		{"fill", KindFill, 0, 16},
		{"fill(2)", KindFill, 0, 32},
		{"fill(0.5)", KindFill, 0, 8},
		{"auto", KindAuto, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			v, diags := parseOne(t, tt.text)
			if diags.HasErrors() {
				t.Fatalf("errors: %v", diags)
			}
			if v.Kind != tt.kind {
				t.Fatalf("kind = %v, want %v", v.Kind, tt.kind)
			}
			if v.Pct != tt.pct || v.Weight != tt.weight {
				t.Fatalf("pct=%d weight=%d, want pct=%d weight=%d", v.Pct, v.Weight, tt.pct, tt.weight)
			}
			if !v.IsSize() {
				t.Fatal("should be usable as a size")
			}
		})
	}
}

func TestParseColors(t *testing.T) {
	tests := []struct {
		text                string
		wantR, wantG, wantB uint8
		wantAlpha           float64
	}{
		{"#ddd", 0xdd, 0xdd, 0xdd, 1},
		{"#1f6feb", 0x1f, 0x6f, 0xeb, 1},
		{"#0008", 0x00, 0x00, 0x00, 0x88 / 255.0},
		{"#1f6feb80", 0x1f, 0x6f, 0xeb, 0x80 / 255.0},
		{"rgb(31 111 235)", 31, 111, 235, 1},
		{"rgb(31, 111, 235)", 31, 111, 235, 1},
		{"slategray", 0x70, 0x80, 0x90, 1},
		{"white", 255, 255, 255, 1},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			v, diags := parseOne(t, tt.text)
			if diags.HasErrors() {
				t.Fatalf("errors: %v", diags)
			}
			if v.Kind != KindColor {
				t.Fatalf("kind = %v, want colour", v.Kind)
			}
			r, g, b := v.Color.ToRGB8()
			if r != tt.wantR || g != tt.wantG || b != tt.wantB {
				t.Fatalf("rgb = %02x%02x%02x, want %02x%02x%02x", r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
			if diff := v.Color.Alpha - tt.wantAlpha; diff > 0.01 || diff < -0.01 {
				t.Fatalf("alpha = %g, want %g", v.Color.Alpha, tt.wantAlpha)
			}
		})
	}
}

// gray() must stay in DeviceGray all the way to the PDF, because that is the
// whole reason the shipped themes use it (DESIGN.md D10).
func TestGrayStaysInDeviceGray(t *testing.T) {
	v, diags := parseOne(t, "gray(0.85)")
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	if v.Color.Space.String() != "gray" {
		t.Fatalf("space = %v, want gray", v.Color.Space)
	}
	if v.Color.Gray != 0.85 {
		t.Fatalf("gray = %g, want 0.85", v.Color.Gray)
	}
	if v.Color.String() != "gray(0.85)" {
		t.Fatalf("round trip = %q", v.Color.String())
	}
}

func TestParseLists(t *testing.T) {
	src := NewSource("t.pulp", "")
	var diags Diagnostics
	text := "1in 0.5in"
	vs := ParseValues(src, Span{Start: 0, End: len(text)}, text, &diags)
	if len(vs) != 2 {
		t.Fatalf("got %d values, want 2", len(vs))
	}
	if vs[0].Length != geom.In(1) || vs[1].Length != geom.In(0.5) {
		t.Fatalf("values = %v", vs)
	}
	// Spans must point into the original argument so a caret can find them.
	if vs[1].Span.Start != 4 {
		t.Fatalf("second value span starts at %d, want 4", vs[1].Span.Start)
	}
}

func TestSeparatorsInsideBracketsDoNotSplit(t *testing.T) {
	src := NewSource("t.pulp", "")
	var diags Diagnostics
	text := `rgb(1, 2, 3) "a b" {x|y z}`
	vs := ParseValues(src, Span{Start: 0, End: len(text)}, text, &diags)
	if len(vs) != 3 {
		var got []string
		for _, v := range vs {
			got = append(got, v.Raw)
		}
		t.Fatalf("got %d values %v, want 3", len(vs), got)
	}
	if vs[1].Str != "a b" {
		t.Fatalf("quoted value = %q", vs[1].Str)
	}
	if vs[2].Kind != KindInterp {
		t.Fatalf("interpolation kind = %v", vs[2].Kind)
	}
}

func TestSingleQuotesAreRaw(t *testing.T) {
	v, _ := parseOne(t, `'{not interpolated}\n'`)
	if v.Str != `{not interpolated}\n` {
		t.Fatalf("raw string = %q", v.Str)
	}
}

func TestDoubleQuotesUnescape(t *testing.T) {
	v, _ := parseOne(t, `"a\tb\nc\{d\}"`)
	if v.Str != "a\tb\nc{d}" {
		t.Fatalf("string = %q", v.Str)
	}
}

// A bare number where a length belongs is the single most likely mistake in
// this language, so it gets a dedicated error rather than a generic one.
func TestBareNumberIsNotALength(t *testing.T) {
	v, diags := parseOne(t, "200")
	if diags.HasErrors() {
		t.Fatalf("200 is a valid number on its own: %v", diags)
	}
	if v.Kind != KindNumber {
		t.Fatalf("kind = %v, want number", v.Kind)
	}
	if v.IsSize() {
		t.Fatal("a bare number must not satisfy a size property")
	}
}

func TestBadValuesReportUsefully(t *testing.T) {
	tests := []struct {
		text     string
		wantCode string
	}{
		{"16qt", "E033"},
		{"#abcde", "E034"},
		{"#gg0000", "E034"},
		{`"unterminated`, "E031"},
		{"gray(0.5, 1, 2)", "E035"},
		{"fill(0)", "E036"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			_, diags := parseOne(t, tt.text)
			if !diags.HasErrors() {
				t.Fatalf("expected an error for %q", tt.text)
			}
			if diags[0].Code != tt.wantCode {
				t.Fatalf("code = %s, want %s (%s)", diags[0].Code, tt.wantCode, diags[0].Message)
			}
		})
	}
}
