package engine

import (
	"math"
	"strconv"
	"strings"
)

// canonicalString returns the canonical string form of v per spec §4.5, and
// reports whether v has one. null, objects, and arrays have none.
func canonicalString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case float64:
		return canonicalNumber(t), true
	default:
		return "", false
	}
}

// canonicalNumber renders f the way ECMAScript's Number::toString does,
// which is what the spec's canonical string form uses for numbers.
func canonicalNumber(f float64) string {
	if f == 0 {
		return "0" // also folds -0 into "0"
	}
	abs := math.Abs(f)
	if abs < 1e21 && abs >= 1e-6 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	return jsExponent(s)
}

// jsExponent rewrites Go's e±DD exponent form (e.g. "1e+21", "1.5e-07")
// into JavaScript's e±D form (e.g. "1e+21", "1.5e-7": no leading zeros).
func jsExponent(s string) string {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return s
	}
	mantissa, exp := s[:i], s[i+1:]
	sign := "+"
	if len(exp) > 0 && (exp[0] == '+' || exp[0] == '-') {
		if exp[0] == '-' {
			sign = "-"
		}
		exp = exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mantissa + "e" + sign + exp
}

// resolveDottedPath resolves a dotted path (spec §4.5) against root, starting
// at root. It returns the resolved value and true, or (nil, false) if
// resolution fails at any segment.
func resolveDottedPath(root any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	segments := strings.Split(path, ".")
	cur := root
	for _, seg := range segments {
		if seg == "" {
			return nil, false
		}
		switch t := cur.(type) {
		case map[string]any:
			v, ok := t[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			idx, ok := canonicalIndex(seg)
			if !ok || idx < 0 || idx >= len(t) {
				return nil, false
			}
			cur = t[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// canonicalIndex parses seg as a canonical base-10 array index: one or more
// ASCII digits, no sign, no leading zeros ("0" itself is canonical).
func canonicalIndex(seg string) (int, bool) {
	if seg == "" {
		return 0, false
	}
	if seg == "0" {
		return 0, true
	}
	if seg[0] == '0' {
		return 0, false
	}
	for _, c := range seg {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(seg)
	if err != nil {
		return 0, false
	}
	return n, true
}
