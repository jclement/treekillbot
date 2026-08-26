package vars

import "testing"

func TestTruthiness(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		want bool
	}{
		{"empty text", NewString(""), false},
		{"text", NewString("notes"), true},
		{"whitespace only", NewString("  "), false},
		// `--var draft=false` arrives as text, and shading the draft watermark
		// anyway would be a genuinely confusing bug.
		{"the word false", NewString("false"), false},
		{"the word no", NewString("No"), false},
		{"the word off", NewString("OFF"), false},
		{"the digit zero", NewString("0"), false},
		{"zero", NewNumber(0), false},
		{"a number", NewNumber(35), true},
		{"true", NewBool(true), true},
		{"false", NewBool(false), false},
		{"an empty list", NewList(nil), false},
		{"a list", NewList([]Value{NewString("a")}), true},
		{"a namespace", NewRecord(Member{"x", NewString("y")}), true},
		{"the zero value", Value{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.Truthy(); got != tt.want {
				t.Fatalf("Truthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultRendering(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		want string
	}{
		{"a whole number has no decimal point", NewNumber(35), "35"},
		{"a fraction keeps its digits", NewNumber(0.25), "0.25"},
		{"a negative number", NewNumber(-3), "-3"},
		{"a boolean", NewBool(true), "true"},
		{"a list joins with commas", NewList([]Value{NewString("a"), NewNumber(2)}), "a, 2"},
		{"a bare namespace has no scalar", NewRecord(Member{"x", NewString("y")}), ""},
		{"the zero value", Value{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoopBindings(t *testing.T) {
	tests := []struct {
		index, count            int
		n, first, last, isCount string
	}{
		{0, 7, "1", "true", "false", "7"},
		{3, 7, "4", "false", "false", "7"},
		{6, 7, "7", "false", "true", "7"},
		{0, 1, "1", "true", "true", "1"},
	}
	for _, tt := range tests {
		loop := NewLoop(tt.index, tt.count)
		for _, check := range []struct{ name, want string }{
			{"n", tt.n},
			{"first", tt.first},
			{"last", tt.last},
			{"count", tt.isCount},
		} {
			if got := member(t, loop, check.name); got != check.want {
				t.Errorf("NewLoop(%d,%d).%s = %q, want %q", tt.index, tt.count, check.name, got, check.want)
			}
		}
		if got := member(t, loop, "index"); got != formatNumber(float64(tt.index)) {
			t.Errorf("loop.index = %q, want %d", got, tt.index)
		}
	}
}

func TestFieldNamesAreInDeclarationOrder(t *testing.T) {
	// Determinism: suggestions and any future `--dump-vars` must not depend on
	// map iteration order.
	v := NewRecord(
		Member{"zebra", NewString("z")},
		Member{"apple", NewString("a")},
	)
	names := v.FieldNames()
	if len(names) != 2 || names[0] != "zebra" || names[1] != "apple" {
		t.Fatalf("FieldNames() = %v, want [zebra apple]", names)
	}
	if _, ok := v.Field("missing"); ok {
		t.Error("Field reported a member that does not exist")
	}
}
