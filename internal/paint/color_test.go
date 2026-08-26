package paint

import "testing"

// D10's promise: a colour is emitted in the space it was authored in, so the
// tint written is the tint the printer receives. A grey that quietly became an
// RGB triple would be converted back by the RIP, and the number that came out
// the far end would not be the one anyone wrote.
func TestGrayStaysInDeviceGray(t *testing.T) {
	c := GrayN(0.85)
	if c.Space != SpaceGray {
		t.Fatalf("space = %v, want gray", c.Space)
	}
	if c.Gray != 0.85 {
		t.Fatalf("gray = %g, want 0.85", c.Gray)
	}
	if got := c.String(); got != "gray(0.85)" {
		t.Fatalf("round trip = %q", got)
	}
}

func TestMixingTwoGreysStaysGrey(t *testing.T) {
	// Mixing in RGB and coming back would lose the exact-tint guarantee for the
	// one case where it is cheapest to keep.
	got := GrayN(0.2).Mix(GrayN(0.8), 0.5)
	if got.Space != SpaceGray {
		t.Fatalf("space = %v, want gray", got.Space)
	}
	if got.Gray != 0.5 {
		t.Fatalf("gray = %g, want 0.5", got.Gray)
	}
}

func TestMixingAcrossSpacesGoesToRGB(t *testing.T) {
	got := GrayN(0).Mix(RGB8(255, 0, 0), 1)
	if got.Space != SpaceRGB {
		t.Fatalf("space = %v, want rgb", got.Space)
	}
	r, g, b := got.ToRGB8()
	if r != 255 || g != 0 || b != 0 {
		t.Fatalf("rgb = %d,%d,%d", r, g, b)
	}
}

func TestTransparentIsNotWhite(t *testing.T) {
	// Painting white still costs a fill and knocks out whatever is beneath it;
	// painting nothing does neither. The distinction matters on paper, where
	// "white" is really "whatever the stock is".
	if !Transparent.IsInvisible() {
		t.Fatal("Transparent must deposit no ink")
	}
	if White.IsInvisible() {
		t.Fatal("White is opaque ink, not absence of ink")
	}
	if Transparent == White {
		t.Fatal("Transparent and White must not be the same value")
	}
}

func TestInkCoverage(t *testing.T) {
	tests := []struct {
		name  string
		color Color
		want  float64
	}{
		{"white deposits nothing", White, 0},
		{"black deposits everything", Black, 1},
		{"mid grey", GrayN(0.5), 0.5},
		{"transparent", Transparent, 0},
		{"half-opaque black", Black.WithAlpha(0.5), 0.5},
		{"registration black", CMYK(1, 1, 1, 1), 1},
		{"plain K", CMYK(0, 0, 0, 1), 0.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.color.InkCoverage()
			if diff := got - tt.want; diff > 0.01 || diff < -0.01 {
				t.Fatalf("coverage = %g, want %g", got, tt.want)
			}
		})
	}
}

func TestDesaturatePreservesAlphaAndReachesGray(t *testing.T) {
	c := RGB8(31, 111, 235).WithAlpha(0.4)
	got := c.Desaturate()
	if got.Space != SpaceGray {
		t.Fatalf("space = %v, want gray", got.Space)
	}
	if got.Alpha != 0.4 {
		t.Fatalf("alpha = %g, want 0.4", got.Alpha)
	}
	// Already-grey colours must be left exactly alone rather than round-tripped.
	grey := GrayN(0.85)
	if grey.Desaturate() != grey {
		t.Fatal("desaturating a grey must be the identity")
	}
}

func TestChannelClamping(t *testing.T) {
	if got := GrayN(1.5); got.Gray != 1 {
		t.Fatalf("gray = %g, want clamped to 1", got.Gray)
	}
	if got := GrayN(-0.5); got.Gray != 0 {
		t.Fatalf("gray = %g, want clamped to 0", got.Gray)
	}
	if got := RGB(2, -1, 0.5); got.R != 1 || got.G != 0 || got.B != 0.5 {
		t.Fatalf("rgb = %g,%g,%g", got.R, got.G, got.B)
	}
}

func TestLuminanceOrdersAsExpected(t *testing.T) {
	if Black.Luminance() >= White.Luminance() {
		t.Fatal("black must be darker than white")
	}
	// Green contributes most to perceived brightness, which is what makes a
	// naive average the wrong way to decide whether a fill is writable.
	green, blue := RGB8(0, 255, 0), RGB8(0, 0, 255)
	if green.Luminance() <= blue.Luminance() {
		t.Fatal("green should read brighter than blue under Rec. 709 weights")
	}
}

func TestStringRendersAsPulpSource(t *testing.T) {
	tests := []struct {
		color Color
		want  string
	}{
		{GrayN(0.85), "gray(0.85)"},
		{RGB8(0x1f, 0x6f, 0xeb), "#1f6feb"},
		{RGB8(0x1f, 0x6f, 0xeb).WithAlpha(0.5), "#1f6feb80"},
		{CMYK(0, 0, 0, 0.13), "cmyk(0 0 0 0.13)"},
	}
	for _, tt := range tests {
		if got := tt.color.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
