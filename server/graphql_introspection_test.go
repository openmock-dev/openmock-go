package server_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

// gqlSchemaDoc references testdata/shop.graphql; served with BaseDir
// "testdata" so the schema resolves.
const gqlSchemaDoc = `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    schema: shop.graphql
    operations:
      - operationType: query
        operationName: GetOrder
        scenarios:
          - name: found
            response:
              data:
                order:
                  id: "{{request.variables.orderId}}"
                  state: SHIPPED
`

func graphqlSchemaServer(t *testing.T, doc, baseDir string) string {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	port := freePort(t)
	srv, err := server.Serve(d, server.Options{
		AdminPortEphemeral: true,
		Ports:              map[string]int{"main": port},
		BaseDir:            baseDir,
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Wait()
	})
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// TestGraphQLIntrospectionSchema pins that, with a schema, a __schema
// introspection query is answered from the schema (spec §3.5) — making the
// mock explorable by GraphQL tooling.
func TestGraphQLIntrospectionSchema(t *testing.T) {
	base := graphqlSchemaServer(t, gqlSchemaDoc, "testdata")

	body := postGraphQL(t, base,
		`{"query":"query IntrospectionQuery { __schema { queryType { name } mutationType { name } types { name kind } } }"}`)

	schema, ok := body["data"].(map[string]any)["__schema"].(map[string]any)
	if !ok {
		t.Fatalf("no __schema in response: %v", body)
	}
	if qt := schema["queryType"].(map[string]any)["name"]; qt != "Query" {
		t.Errorf("queryType.name = %v, want Query", qt)
	}
	if mt := schema["mutationType"].(map[string]any)["name"]; mt != "Mutation" {
		t.Errorf("mutationType.name = %v, want Mutation", mt)
	}
	// The declared types must be discoverable.
	names := map[string]bool{}
	for _, tp := range schema["types"].([]any) {
		names[tp.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"Query", "Mutation", "Order", "State"} {
		if !names[want] {
			t.Errorf("introspection types missing %q; got %v", want, names)
		}
	}
}

// TestGraphQLIntrospectionType pins the __type(name:) root field.
func TestGraphQLIntrospectionType(t *testing.T) {
	base := graphqlSchemaServer(t, gqlSchemaDoc, "testdata")

	body := postGraphQL(t, base,
		`{"query":"{ __type(name: \"Order\") { name kind fields { name } } }"}`)
	typ, ok := body["data"].(map[string]any)["__type"].(map[string]any)
	if !ok {
		t.Fatalf("no __type in response: %v", body)
	}
	if typ["name"] != "Order" || typ["kind"] != "OBJECT" {
		t.Errorf("__type = %v, want Order/OBJECT", typ)
	}

	// An unknown type resolves to null.
	body = postGraphQL(t, base, `{"query":"{ __type(name: \"Nope\") { name } }"}`)
	if v := body["data"].(map[string]any)["__type"]; v != nil {
		t.Errorf("__type(Nope) = %v, want null", v)
	}
}

// TestGraphQLIntrospectionDoesNotChangeResolution pins spec §3.5: a normal
// request resolves through the engine exactly as it would without a schema —
// introspection is purely additive.
func TestGraphQLIntrospectionDoesNotChangeResolution(t *testing.T) {
	base := graphqlSchemaServer(t, gqlSchemaDoc, "testdata")
	body := postGraphQL(t, base,
		`{"query":"query GetOrder { order { id state } }","variables":{"orderId":"42"}}`)
	order := body["data"].(map[string]any)["order"].(map[string]any)
	if order["id"] != "42" || order["state"] != "SHIPPED" {
		t.Errorf("order = %v, want id 42 / SHIPPED (engine-resolved, schema irrelevant)", order)
	}
}

// TestGraphQLMissingSchemaIsLenient pins spec §3.5: a schema that cannot be
// loaded is a diagnostic, not an error — the server still serves, just without
// introspection (the introspection query falls through to the engine and is
// unrouted).
func TestGraphQLMissingSchemaIsLenient(t *testing.T) {
	doc := `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    schema: does-not-exist.graphql
    operations:
      - operationType: query
        operationName: GetOrder
        scenarios:
          - name: found
            response:
              data:
                order: {id: "1"}
`
	base := graphqlSchemaServer(t, doc, "testdata") // Serve must not fail

	// Without a loaded schema, an introspection query is not answered from the
	// schema; it routes through the engine and is unrouted.
	body := postGraphQL(t, base, `{"query":"{ __schema { queryType { name } } }"}`)
	if _, hasData := body["data"]; hasData {
		t.Errorf("expected unrouted errors-only response, got data: %v", body)
	}
	if _, hasErrors := body["errors"]; !hasErrors {
		t.Errorf("expected errors for introspection without a schema, got %v", body)
	}
}
