package engine

import (
	"net/url"
	"strings"
)

// pathSegment is one segment of a compiled path template (spec §5.2).
type pathSegment struct {
	literal string
	isParam bool
	param   string
}

// compiledPath is a path template split into segments, ready to match
// against a concrete request path.
type compiledPath []pathSegment

func compilePath(template string) compiledPath {
	trimmed := strings.TrimPrefix(template, "/")
	parts := strings.Split(trimmed, "/")
	segs := make(compiledPath, len(parts))
	for i, p := range parts {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") && len(p) > 2 {
			segs[i] = pathSegment{isParam: true, param: p[1 : len(p)-1]}
		} else {
			segs[i] = pathSegment{literal: p}
		}
	}
	return segs
}

// match compares the compiled template against a concrete request path
// (spec §5.2). It returns the captured path parameters and whether the
// template matched.
func (c compiledPath) match(requestPath string) (map[string]string, bool) {
	trimmed := strings.TrimPrefix(requestPath, "/")
	rawParts := strings.Split(trimmed, "/")
	if len(rawParts) != len(c) {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range c {
		decoded := percentDecodeSegment(rawParts[i])
		if seg.isParam {
			if decoded == "" {
				return nil, false
			}
			params[seg.param] = decoded
		} else if seg.literal != decoded {
			return nil, false
		}
	}
	return params, true
}

// percentDecodeSegment decodes a single path segment per spec §5.2: '%XX'
// escapes decode to the octet, '+' stays literal, and a malformed escape
// leaves the segment as written.
func percentDecodeSegment(seg string) string {
	decoded, err := url.PathUnescape(seg)
	if err != nil {
		return seg
	}
	return decoded
}
