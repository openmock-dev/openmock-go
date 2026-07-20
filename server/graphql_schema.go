package server

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openmock-dev/openmock-go/engine"
)

// graphqlIntrospection answers GraphQL introspection queries from a server's
// schema (spec §3.5). It is an *additional* capability: it never changes how a
// normal request resolves — that stays the engine's job — it only lets GraphQL
// tooling (GraphiQL, Apollo, Postman, …) explore the mock. When a server has no
// schema, or the schema cannot be loaded, this is nil and introspection is
// simply unavailable (spec §3.5: a missing/unreadable schema is not an error).
type graphqlIntrospection struct {
	schema any   // the __schema introspection object
	types  []any // __schema.types, for __type(name:) lookups
}

// loadGraphQLSchema resolves a GraphQL server's schema path (confined to the
// document directory, spec §14), parses the SDL, and precomputes the
// introspection result. The graph-gophers parser is used only to inspect the
// schema (a nil resolver means it is never executed), so no resolvers are
// needed. Any failure is returned so the caller can serve without
// introspection.
func loadGraphQLSchema(baseDir string, srv *engine.Server) (*graphqlIntrospection, error) {
	path, err := confinedPath(baseDir, srv.Schema, "schema")
	if err != nil {
		return nil, err
	}
	sdl, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := graphql.ParseSchema(string(sdl), nil)
	if err != nil {
		return nil, fmt.Errorf("parsing schema %s: %w", srv.Schema, err)
	}
	introspectionJSON, err := parsed.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("introspecting schema %s: %w", srv.Schema, err)
	}

	var wrapper struct {
		Schema json.RawMessage `json:"__schema"`
	}
	if err := json.Unmarshal(introspectionJSON, &wrapper); err != nil {
		return nil, err
	}
	var schemaObj any
	if err := json.Unmarshal(wrapper.Schema, &schemaObj); err != nil {
		return nil, err
	}
	gi := &graphqlIntrospection{schema: schemaObj}
	if m, ok := schemaObj.(map[string]any); ok {
		gi.types, _ = m["types"].([]any)
	}
	return gi, nil
}

// answer returns the introspection response for a query that asks for
// __schema and/or __type, and reports whether the query was an introspection
// query at all. A non-introspection query returns (nil, false) and is left to
// the engine. Following OpenMock's model (a response is not filtered by the
// request's selection set, spec §4.3), the full __schema object is returned
// whenever __schema is requested.
func (g *graphqlIntrospection) answer(query string, variables any) (map[string]any, bool) {
	wantsSchema, wantsType := introspectionTargets(query)
	if !wantsSchema && !wantsType {
		return nil, false
	}
	data := map[string]any{}
	if wantsSchema {
		data["__schema"] = g.schema
	}
	if wantsType {
		if name, ok := graphqlTypeArg(query, variables); ok {
			data["__type"] = g.typeByName(name)
		} else {
			data["__type"] = nil
		}
	}
	return map[string]any{"data": data}, true
}

func (g *graphqlIntrospection) typeByName(name string) any {
	for _, t := range g.types {
		if m, ok := t.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	return nil // GraphQL: __type returns null for an unknown type
}

// introspectionTargets reports whether a query selects the __schema and/or
// __type introspection root fields. The double-underscore prefix is reserved
// for introspection, so a name token of exactly "__schema" or "__type"
// identifies one; "__typename" is a different token and is not treated as a
// schema-exploration request.
func introspectionTargets(query string) (schema, typ bool) {
	s := &gqlScanner{src: query}
	for {
		tok, ok := s.next()
		if !ok {
			return
		}
		if tok.kind == tokName {
			switch tok.text {
			case "__schema":
				schema = true
			case "__type":
				typ = true
			}
		}
	}
}

// typeArgPattern matches a __type root field's name argument, as a string
// literal or a variable reference.
var typeArgPattern = regexp.MustCompile(`__type\s*\(\s*name\s*:\s*(?:"([^"]*)"|\$([A-Za-z_][A-Za-z0-9_]*))`)

// graphqlTypeArg extracts the name argument of a __type introspection field:
// a string literal directly, or a variable resolved from the request's
// variables.
func graphqlTypeArg(query string, variables any) (string, bool) {
	m := typeArgPattern.FindStringSubmatch(query)
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1], true
	}
	vars, ok := variables.(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := vars[m[2]].(string)
	return name, ok
}
