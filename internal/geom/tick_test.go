package geom

import "testing"

func TestUnitConversions(t *testing.T) {
	tests := []struct {
		name string
		got  Tick
		want Tick
	}{
		{"one point", Pt(1), 16},
		{"one inch", In(1), 1152},
		{"half inch", In(0.5), 576},
		{"one pica", Pc(1), 192},
		{"96 css pixels is an inch", Px(96), 1152},
		// A4 is 210x297mm. The nominal point value is 595.276; we land on
		// 595.25pt, which is 9524 ticks. The 0.009mm difference is invisible on
		// paper and buys an exactly divisible grid.
		{"a4 width", Mm(210), 9524},
		{"a4 height", Mm(297), 13470},
		{"one centimetre", Cm(1), 454},
		{"rounds half away from zero", Pt(0.03125), 1},
		{"negative rounds away from zero", Pt(-0.03125), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %d ticks, want %d", tt.got, tt.want)
			}
		})
	}
}

func TestPointsRoundTripIsLossless(t *testing.T) {
	// Every tick value must survive a trip through float64 points, because the
	// PDF writer emits points with %.4f and 1/16 is exact in 4 decimals.
	for i := Tick(-5000); i <= 5000; i++ {
		if got := Pt(i.Points()); got != i {
			t.Fatalf("tick %d round-tripped to %d", i, got)
		}
	}
}

func TestScaleRoundsHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		t        Tick
		num, den int64
		want     Tick
	}{
		{100, 62, 100, 62}, // the checkbox metric: 0.62 * line-height
		{3, 1, 2, 2},       // 1.5 rounds to 2
		{-3, 1, 2, -2},     // -1.5 rounds to -2, not -1
		{5, 1, 2, 3},       // 2.5 rounds to 3
		{240, 1, 3, 80},    // exact division is untouched
		{0, 7, 3, 0},
	}
	for _, tt := range tests {
		if got := tt.t.Scale(tt.num, tt.den); got != tt.want {
			t.Fatalf("Tick(%d).Scale(%d,%d) = %d, want %d", tt.t, tt.num, tt.den, got, tt.want)
		}
	}
}

func TestClampTreatsInvertedBoundsAsMinimumWins(t *testing.T) {
	if got := Clamp(50, 100, 20); got != 100 {
		t.Fatalf("Clamp(50,100,20) = %d, want 100 (min wins over a smaller max)", got)
	}
	if got := Clamp(50, 0, 0); got != 50 {
		t.Fatalf("Clamp with absent max = %d, want 50", got)
	}
	if got := Clamp(500, 0, 100); got != 100 {
		t.Fatalf("Clamp(500,0,100) = %d, want 100", got)
	}
}
