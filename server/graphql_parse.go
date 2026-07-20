package server

import (
	"fmt"
	"strings"
)

// gqlOperation is one operation definition found in a GraphQL document.
type gqlOperation struct {
	opType string // "query" or "mutation" (v0.2 has no subscription serving)
	name   string // "" for an anonymous shorthand operation
}

// selectOperation resolves which operation a GraphQL-over-HTTP request runs
// (spec §4.3): the operation named by operationName, or — when the body omits
// it — the document's single operation. Deriving the operation type and name
// from the query text (rather than a schema) is exactly the implementation
// concern the spec leaves open.
func selectOperation(query, operationName string) (opType, opName string, err error) {
	ops, err := scanOperations(query)
	if err != nil {
		return "", "", err
	}
	if len(ops) == 0 {
		return "", "", fmt.Errorf("no operation in query")
	}

	if operationName != "" {
		for _, op := range ops {
			if op.name == operationName {
				return op.opType, op.name, nil
			}
		}
		return "", "", fmt.Errorf("no operation named %q in query", operationName)
	}
	if len(ops) > 1 {
		return "", "", fmt.Errorf("query has multiple operations; operationName is required")
	}
	return ops[0].opType, ops[0].name, nil
}

// scanOperations returns the top-level operation definitions of a GraphQL
// executable document, in order. It ignores fragment definitions, and is
// aware of GraphQL comments (# to end of line), string and block-string
// literals, and brace/paren nesting, so operations are found only at the top
// level. It does not fully validate the document — a mock only needs each
// operation's type and name.
//
// The scan tracks two things beyond token identity: brace depth (a definition
// body sits at depth > 0) and paren depth (a variable-definition or argument
// list, whose object default values `{ … }` must not be mistaken for a
// selection set). A depth-0, paren-0 `{` is the current definition's body when
// one was just opened by a keyword, and otherwise an anonymous shorthand
// query.
func scanOperations(src string) ([]gqlOperation, error) {
	var ops []gqlOperation
	s := &gqlScanner{src: src}
	braces, parens := 0, 0
	pendingBody := false // a keyword definition is awaiting its selection set

	for {
		tok, ok := s.next()
		if !ok {
			break
		}
		switch tok.kind {
		case tokLParen:
			parens++
		case tokRParen:
			if parens > 0 {
				parens--
			}
		case tokLBrace:
			if parens == 0 && braces == 0 {
				if pendingBody {
					pendingBody = false // this brace is the current def's body
				} else {
					ops = append(ops, gqlOperation{opType: "query"}) // anonymous shorthand
				}
			}
			if parens == 0 {
				braces++
			}
		case tokRBrace:
			if parens == 0 {
				if braces == 0 {
					return nil, fmt.Errorf("unbalanced '}' in query")
				}
				braces--
			}
		case tokName:
			if parens != 0 || braces != 0 {
				continue
			}
			switch tok.text {
			case "query", "mutation", "subscription":
				op := gqlOperation{opType: tok.text}
				// An optional operation name follows, before the variable
				// definitions '(', a directive '@', or the selection set '{'.
				if n, ok := s.peekName(); ok {
					op.name = n
				}
				ops = append(ops, op)
				pendingBody = true
			case "fragment":
				// A fragment definition is not an operation, but its selection
				// set must still be consumed so the next '{' is not read as an
				// anonymous operation.
				pendingBody = true
			}
		}
	}
	if braces != 0 {
		return nil, fmt.Errorf("unbalanced '{' in query")
	}
	return ops, nil
}

// --- a minimal GraphQL lexer, enough for operation discovery ---

type gqlTokenKind int

const (
	tokName gqlTokenKind = iota
	tokLBrace
	tokRBrace
	tokLParen
	tokRParen
	tokOther // punctuation we don't distinguish (colons, dollars, etc.)
)

type gqlToken struct {
	kind gqlTokenKind
	text string // set for tokName
}

type gqlScanner struct {
	src string
	pos int
}

// next returns the next significant token, skipping insignificant characters
// (whitespace, commas, BOM), comments, and string/block-string literals.
func (s *gqlScanner) next() (gqlToken, bool) {
	s.skipIgnored()
	if s.pos >= len(s.src) {
		return gqlToken{}, false
	}
	c := s.src[s.pos]
	switch {
	case c == '{':
		s.pos++
		return gqlToken{kind: tokLBrace}, true
	case c == '}':
		s.pos++
		return gqlToken{kind: tokRBrace}, true
	case c == '(':
		s.pos++
		return gqlToken{kind: tokLParen}, true
	case c == ')':
		s.pos++
		return gqlToken{kind: tokRParen}, true
	case isNameStart(c):
		start := s.pos
		for s.pos < len(s.src) && isNameContinue(s.src[s.pos]) {
			s.pos++
		}
		return gqlToken{kind: tokName, text: s.src[start:s.pos]}, true
	default:
		s.pos++
		return gqlToken{kind: tokOther}, true
	}
}

// peekName returns the next token's name if it is a Name, consuming it; if the
// next token is not a Name it is left unconsumed and ok is false. Used to read
// an operation's optional name right after its keyword.
func (s *gqlScanner) peekName() (string, bool) {
	save := s.pos
	tok, ok := s.next()
	if ok && tok.kind == tokName {
		return tok.text, true
	}
	s.pos = save
	return "", false
}

// skipIgnored advances past whitespace, commas, BOM, comments, and string and
// block-string literals — everything that cannot be an operation keyword,
// name, or brace.
func (s *gqlScanner) skipIgnored() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			s.pos++
		case c == '#':
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
		case c == '"':
			s.skipString()
		default:
			return
		}
	}
}

// skipString consumes a string or block-string literal starting at the
// current '"'.
func (s *gqlScanner) skipString() {
	if strings.HasPrefix(s.src[s.pos:], `"""`) {
		s.pos += 3
		for s.pos < len(s.src) {
			if strings.HasPrefix(s.src[s.pos:], `"""`) {
				s.pos += 3
				return
			}
			if s.src[s.pos] == '\\' {
				s.pos++ // skip an escaped char (e.g. \""")
			}
			s.pos++
		}
		return
	}
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == '\\' {
			s.pos += 2
			continue
		}
		if c == '"' || c == '\n' {
			s.pos++
			return
		}
		s.pos++
	}
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameContinue(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9')
}
