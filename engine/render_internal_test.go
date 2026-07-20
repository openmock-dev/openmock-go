package engine

import (
	"reflect"
	"testing"
)

// TestScanPlaceholders pins placeholder scanning (spec §9, §9.5): the
// closing }} is found honouring quoted literals, unterminated openers and
// quotes never produce a placeholder, and offsets/trimming are exact.
func TestScanPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []placeholder
	}{
		{"plain", "{{a}}", []placeholder{{0, 5, "a"}}},
		{"trimmed", "{{ params.id }}", []placeholder{{0, 15, "params.id"}}},
		{"adjacent", "{{a}}{{b}}", []placeholder{{0, 5, "a"}, {5, 10, "b"}}},
		{"embedded", "x{{a}}y", []placeholder{{1, 6, "a"}}},
		{"close brace in single quotes", `{{ 'a}}b' }}`, []placeholder{{0, 12, `'a}}b'`}}},
		{"close brace in double quotes", `{{ "}}" }}`, []placeholder{{0, 10, `"}}"`}}},
		{"unterminated opener", "{{ foo", nil},
		{"unterminated quote swallows to end", `{{ 'foo }} bar`, nil},
		// The leftmost {{ opens the placeholder and an unquoted }} closes
		// it — a nested {{ is just expression text. This mirrors the
		// escaping model of spec §9.5: a literal {{ must be written as a
		// quoted literal, so it is never treated as a nested opener.
		{"inner braces are expression text", "{{ foo {{bar}}", []placeholder{{0, 14, "foo {{bar"}}},
		{"no placeholders", "plain text }} {", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanPlaceholders(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scanPlaceholders(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
