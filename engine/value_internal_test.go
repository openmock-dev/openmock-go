package engine

import (
	"math"
	"testing"
)

// TestCanonicalNumber pins the canonical string form of numbers (spec §4.5):
// the shortest decimal that round-trips the IEEE 754 double, matching
// ECMAScript Number::toString — including the exponent-notation thresholds
// (>= 1e21, < 1e-6) and the -0 fold.
func TestCanonicalNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{1, "1"},
		{-1.5, "-1.5"},
		{0.1, "0.1"},
		{0.5, "0.5"},
		{42, "42"},
		{1e20, "100000000000000000000"},
		{1e21, "1e+21"},
		{-1e21, "-1e+21"},
		{1e-6, "0.000001"},
		{1e-7, "1e-7"},
		{1.5e-7, "1.5e-7"},
		{1e100, "1e+100"},
		{float64(1 << 53), "9007199254740992"},
	}
	for _, tc := range cases {
		if got := canonicalNumber(tc.in); got != tc.want {
			t.Errorf("canonicalNumber(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJSExponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1e+21", "1e+21"},
		{"1.5e-07", "1.5e-7"},
		{"1e-007", "1e-7"},
		{"1e+00", "1e+0"},
		{"123", "123"}, // no exponent: passthrough
	}
	for _, tc := range cases {
		if got := jsExponent(tc.in); got != tc.want {
			t.Errorf("jsExponent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCanonicalIndex pins the canonical base-10 array-index rule of spec
// §4.5: ASCII digits only, no sign, no leading zeros ("0" is canonical).
func TestCanonicalIndex(t *testing.T) {
	valid := map[string]int{"0": 0, "1": 1, "10": 10, "42": 42}
	for in, want := range valid {
		got, ok := canonicalIndex(in)
		if !ok || got != want {
			t.Errorf("canonicalIndex(%q) = (%d, %v), want (%d, true)", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "00", "01", "007", "-1", "+1", "1.0", "1a", "a"} {
		if _, ok := canonicalIndex(in); ok {
			t.Errorf("canonicalIndex(%q) accepted a non-canonical segment", in)
		}
	}
}

// TestResolveDottedPath pins dotted-path resolution (spec §4.5): object
// members and canonical array indices, with every failure mode resolving to
// absent rather than an error.
func TestResolveDottedPath(t *testing.T) {
	root := map[string]any{
		"items": []any{
			map[string]any{"id": "a1", "qty": float64(2)},
		},
		"user": map[string]any{"role": "admin"},
		"note": "text",
	}

	if v, ok := resolveDottedPath(root, "items.0.qty"); !ok || v != float64(2) {
		t.Errorf("items.0.qty = (%v, %v), want (2, true)", v, ok)
	}
	if v, ok := resolveDottedPath(root, "user.role"); !ok || v != "admin" {
		t.Errorf("user.role = (%v, %v), want (admin, true)", v, ok)
	}

	for _, path := range []string{
		"",           // empty path
		"items.1.id", // index out of range
		"items.01",   // non-canonical index
		"items.-1",   // signed index
		"note.x",     // traversal through a scalar
		"missing",    // absent member
		"user..role", // empty segment
	} {
		if v, ok := resolveDottedPath(root, path); ok {
			t.Errorf("resolveDottedPath(%q) = (%v, true), want absent", path, v)
		}
	}
}
