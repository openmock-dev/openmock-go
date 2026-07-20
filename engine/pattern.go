package engine

import "regexp"

// compilePattern compiles src as an RE2 pattern (spec §6.1), anchored to
// match an entire string. Go's regexp package is itself RE2-based and
// already rejects backreferences and lookaround, so regexp.Compile is the
// full validity check the spec requires.
func compilePattern(src string) (*regexp.Regexp, error) {
	return regexp.Compile(`\A(?:` + src + `)\z`)
}
