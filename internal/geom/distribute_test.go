package geom

import (
	"math/rand"
	"testing"
)

// The whole point of DistributeTicks is this one property. If it ever fails,
// panels stop lining up and no amount of care elsewhere recovers it.
func TestDistributeTicksSumsExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 20000; trial++ {
		n := 1 + rng.Intn(12)
		weights := make([]int32, n)
		anyPositive := false
		for i := range weights {
			// Deliberately include zero and negative weights: callers pass them
			// for collapsed children and must not have to filter first.
			weights[i] = int32(rng.Intn(400) - 20)
			if weights[i] > 0 {
				anyPositive = true
			}
		}
		total := Tick(rng.Intn(200000) - 100000)

		shares := DistributeTicks(total, weights)

		var sum Tick
		for i, s := range shares {
			sum += s
			if weights[i] <= 0 && s != 0 {
				t.Fatalf("non-positive weight %d received %d ticks", weights[i], s)
			}
		}
		want := total
		if !anyPositive {
			want = 0
		}
		if sum != want {
			t.Fatalf("weights=%v total=%d: shares %v sum to %d, want %d", weights, total, shares, sum, want)
		}
	}
}

func TestDistributeTicksKnownCases(t *testing.T) {
	tests := []struct {
		name    string
		total   Tick
		weights []int32
		want    []Tick
	}{
		// The canonical three-way split: 1600 ticks (100pt) does not divide by
		// three, and the extra tick goes to the first share, not to a float.
		{"three equal fills", 1600, []int32{1, 1, 1}, []Tick{534, 533, 533}},
		// A 30/70 column split of a content width must consume it entirely.
		{"thirty seventy", 9524, []int32{3000, 7000}, []Tick{2857, 6667}},
		{"weighted fill", 1000, []int32{16, 32}, []Tick{333, 667}},
		{"single share takes all", 777, []int32{5}, []Tick{777}},
		{"zero weight excluded", 100, []int32{1, 0, 1}, []Tick{50, 0, 50}},
		{"no positive weights", 100, []int32{0, 0}, []Tick{0, 0}},
		{"empty", 100, nil, []Tick{}},
		// Seven day cells across a landscape Letter content width.
		{"seven days", 5000, []int32{1, 1, 1, 1, 1, 1, 1}, []Tick{715, 715, 714, 714, 714, 714, 714}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DistributeTicks(tt.total, tt.weights)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// Ties must resolve by index so that output depends only on inputs. Six ticks
// among four equal shares leaves two over; they go to shares 0 and 1.
func TestDistributeTicksTieBreakByIndex(t *testing.T) {
	got := DistributeTicks(6, []int32{1, 1, 1, 1})
	want := []Tick{2, 2, 1, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDistributeTicksIsDeterministic(t *testing.T) {
	weights := []int32{3000, 2200, 4400, 400}
	first := DistributeTicks(9524, weights)
	for i := 0; i < 1000; i++ {
		again := DistributeTicks(9524, weights)
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("run %d differs: %v vs %v", i, again, first)
			}
		}
	}
}

func TestCumulativeOffsetsNoTrailingGap(t *testing.T) {
	sizes := []Tick{100, 200, 300}
	offsets, total := CumulativeOffsets(sizes, 16)
	wantOffsets := []Tick{0, 116, 332}
	for i := range wantOffsets {
		if offsets[i] != wantOffsets[i] {
			t.Fatalf("offsets = %v, want %v", offsets, wantOffsets)
		}
	}
	if total != 632 { // 600 of content plus two gaps, not three
		t.Fatalf("total = %d, want 632", total)
	}
}

func TestDistributeEqual(t *testing.T) {
	shares := DistributeEqual(1000, 7)
	var sum Tick
	for _, s := range shares {
		sum += s
	}
	if sum != 1000 {
		t.Fatalf("DistributeEqual sum = %d, want 1000", sum)
	}
	if len(shares) != 7 {
		t.Fatalf("len = %d, want 7", len(shares))
	}
	if got := DistributeEqual(100, 0); got != nil {
		t.Fatalf("DistributeEqual(100, 0) = %v, want nil", got)
	}
}
