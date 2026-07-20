package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

// graphqlServer drives the GraphQL handler through Serve (the handler is
// unexported) on an ephemeral port and returns the mock server's base URL.
func graphqlServer(t *testing.T, doc string) string {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	port := freePort(t) // defined in serve_test.go (same package)
	srv, err := server.Serve(d, server.Options{AdminPortEphemeral: true, Ports: map[string]int{"main": port}})
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

const gqlDoc = `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    operations:
      - operationType: query
        operationName: GetOrder
        scenarios:
          - name: admin
            when:
              headers:
                x-role: admin
            response:
              data:
                order:
                  id: "{{request.variables.orderId}}"
                  state: SHIPPED
                  internalNotes: secret
          - name: missing
            when:
              variables:
                orderId: "0"
            response:
              data:
                order: null
              errors:
                - message: order not found
                  path: [order]
                  extensions:
                    code: NOT_FOUND
          - name: default
            response:
              data:
                order:
                  id: "{{request.variables.orderId}}"
                  state: SHIPPED
      - operationType: mutation
        operationName: CreateOrder
        scenarios:
          - name: created
            response:
              data:
                createOrder:
                  id: "new-1"
                  note: "{{request.variables.note}}"
`

func TestGraphQLQuery(t *testing.T) {
	base := graphqlServer(t, gqlDoc)

	// Default scenario: variables templating.
	body := postGraphQL(t, base, `{"query":"query GetOrder($id: ID!){ order(id:$id){ id state } }","variables":{"orderId":"42"}}`)
	order := body["data"].(map[string]any)["order"].(map[string]any)
	if order["id"] != "42" || order["state"] != "SHIPPED" {
		t.Errorf("order = %v, want id 42 / SHIPPED", order)
	}

	// Header facet selects the admin scenario.
	admin := postGraphQLWith(t, base,
		`{"query":"query GetOrder { order { id } }","variables":{"orderId":"7"}}`,
		map[string]string{"X-Role": "admin"})
	adminOrder := admin["data"].(map[string]any)["order"].(map[string]any)
	if adminOrder["internalNotes"] != "secret" {
		t.Errorf("admin order = %v, want internalNotes secret", adminOrder)
	}
}

func TestGraphQLPartialErrors(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	body := postGraphQL(t, base, `{"query":"query GetOrder { order { id } }","variables":{"orderId":"0"}}`)

	if data := body["data"].(map[string]any); data["order"] != nil {
		t.Errorf("data.order = %v, want null", data["order"])
	}
	errs, ok := body["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("errors = %v, want one error", body["errors"])
	}
	if msg := errs[0].(map[string]any)["message"]; msg != "order not found" {
		t.Errorf("error message = %v, want 'order not found'", msg)
	}
}

func TestGraphQLMutationRouting(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	body := postGraphQL(t, base, `{"query":"mutation CreateOrder($note: String){ createOrder(note:$note){ id note } }","variables":{"note":"rush"}}`)
	created := body["data"].(map[string]any)["createOrder"].(map[string]any)
	if created["id"] != "new-1" || created["note"] != "rush" {
		t.Errorf("createOrder = %v, want id new-1 / note rush", created)
	}
}

// TestGraphQLUnrouted pins that a query naming an operation the document does
// not declare gets the errors-only unrouted synthetic (spec §10.2), still
// over HTTP 200.
func TestGraphQLUnrouted(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	resp, code := postGraphQLRaw(t, base, `{"query":"query Nope { nope }"}`)
	if code != 200 {
		t.Errorf("status = %d, want 200 (errors are in-band)", code)
	}
	var body map[string]any
	json.Unmarshal(resp, &body)
	if _, hasData := body["data"]; hasData {
		t.Errorf("unrouted response carried data: %v", body)
	}
	errs := body["errors"].([]any)
	ext := errs[0].(map[string]any)["extensions"].(map[string]any)
	if ext["openmock"] != "unrouted" || ext["operationName"] != "Nope" {
		t.Errorf("extensions = %v, want openmock unrouted / operationName Nope", ext)
	}
}

// TestGraphQLGet pins GET support: query and variables come from the query
// string.
func TestGraphQLGet(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	u := base + "/?" + url.Values{
		"query":     {"query GetOrder { order { id } }"},
		"variables": {`{"orderId":"9"}`},
	}.Encode()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	order := body["data"].(map[string]any)["order"].(map[string]any)
	if order["id"] != "9" {
		t.Errorf("order.id = %v, want 9", order["id"])
	}
}

func TestGraphQLBadRequest(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	// Missing query.
	_, code := postGraphQLRaw(t, base, `{}`)
	if code != 400 {
		t.Errorf("empty query: status = %d, want 400", code)
	}
	// Malformed JSON.
	_, code = postGraphQLRaw(t, base, `{not json`)
	if code != 400 {
		t.Errorf("bad JSON: status = %d, want 400", code)
	}
}

// --- helpers ---

func postGraphQL(t *testing.T, base, body string) map[string]any {
	t.Helper()
	return postGraphQLWith(t, base, body, nil)
}

func postGraphQLWith(t *testing.T, base, body string, headers map[string]string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/graphql", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func postGraphQLRaw(t *testing.T, base, body string) ([]byte, int) {
	t.Helper()
	resp, err := http.Post(base+"/graphql", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode
}

// TestGraphQLServeMethodNotAllowed pins that a non GET/POST method is 405.
func TestGraphQLServeMethodNotAllowed(t *testing.T) {
	base := graphqlServer(t, gqlDoc)
	req, _ := http.NewRequest("DELETE", base+"/graphql", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	if resp.Header.Get("Allow") == "" {
		t.Errorf("missing Allow header on 405")
	}
}
