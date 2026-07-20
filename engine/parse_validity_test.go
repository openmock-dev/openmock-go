package engine_test

import (
	"strings"
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

// TestParseValidity pins the load-time validity rules of spec §3.6 that the
// conformance corpus does not cover: every JSON Schema constraint
// (schema/openmock-0.2.0.json in the spec repo) is a load-time refusal, so
// Parse must reject each invalid document below — and must keep accepting
// its minimal valid twin where a boundary matters.
func TestParseValidity(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string // empty: Parse must accept; else: substring of the error
	}{
		// --- port range 1-65535 ---
		{"port zero", httpDoc(`port: 0`), "'port'"},
		{"port negative", httpDoc(`port: -1`), "'port'"},
		{"port too large", httpDoc(`port: 65536`), "'port'"},
		{"port min ok", httpDoc(`port: 1`), ""},
		{"port max ok", httpDoc(`port: 65535`), ""},

		// --- HTTP status range 100-599 ---
		{"status below range", httpDocStatus("99"), "'status'"},
		{"status above range", httpDocStatus("600"), "'status'"},
		{"status min ok", httpDocStatus("100"), ""},
		{"status max ok", httpDocStatus("599"), ""},

		// --- close.code range 1000-4999 ---
		{"close code below range", wsDocClose("999"), "'close.code'"},
		{"close code above range", wsDocClose("5000"), "'close.code'"},
		{"close code min ok", wsDocClose("1000"), ""},
		{"close code max ok", wsDocClose("4999"), ""},

		// --- method pattern ^[A-Za-z]+$ ---
		{"method with digit", httpDocMethod("GET1"), "'method'"},
		{"method with space", httpDocMethod("GE T"), "'method'"},
		{"method empty", httpDocMethod(`""`), "'method'"},
		{"method lowercase ok", httpDocMethod("get"), ""},

		// --- minLength: 1 on names and attachments ---
		{"empty service", grpcDoc(`service: ""`, `rpc: GetOrder`), "'service'"},
		{"empty rpc", grpcDoc(`service: shop.v1.OrderService`, `rpc: ""`), "'rpc'"},
		{"empty operationName", `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    operations:
      - operationName: ""
        scenarios:
          - name: ok
            response:
              data: {ok: true}
`, "'operationName'"},
		{"empty descriptorSet", `
openmock: 0.2.0
servers:
  - name: main
    type: grpc
    descriptorSet: ""
    operations:
      - service: shop.v1.OrderService
        rpc: GetOrder
        type: unary
        scenarios:
          - name: ok
            response:
              status: OK
              message: {ok: true}
`, "'descriptorSet'"},
		{"empty schema", `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    schema: ""
    operations:
      - operationName: GetOrder
        scenarios:
          - name: ok
            response:
              data: {ok: true}
`, "'schema'"},
		{"descriptorSet on http server", httpDoc(`descriptorSet: ./shop.binpb`), "'descriptorSet'"},
		{"schema on grpc server", `
openmock: 0.2.0
servers:
  - name: main
    type: grpc
    schema: ./shop.graphql
    operations:
      - service: shop.v1.OrderService
        rpc: GetOrder
        type: unary
        scenarios:
          - name: ok
            response:
              status: OK
              message: {ok: true}
`, "'schema'"},

		// --- when: minProperties 1, no x- keys ---
		{"empty when", httpDocWhen(`{}`), "'when'"},
		{"when with only x- key", httpDocWhen(`{x-note: 1}`), "facet"},

		// --- x- keys allowed exactly where spec §11 permits ---
		{"x- on info", `
openmock: 0.2.0
info:
  x-team: payments
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response:
              status: 200
`, "x-team"},
		{"x- at permitted levels", `
openmock: 0.2.0
x-root: 1
servers:
  - name: main
    type: http
    x-server: 1
    operations:
      - method: GET
        path: /ping
        x-operation: 1
        scenarios:
          - name: ok
            x-scenario: 1
            response:
              status: 200
              x-response: 1
`, ""},

		// --- response header list minItems: 1 ---
		{"empty header list", `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response:
              status: 200
              headers:
                Set-Cookie: []
`, "header"},
		{"one-item header list ok", `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response:
              status: 200
              headers:
                Set-Cookie:
                  - "a=1; Path=/"
`, ""},

		// --- non-list messages ---
		{"grpc messages non-list", `
openmock: 0.2.0
servers:
  - name: main
    type: grpc
    operations:
      - service: shop.v1.OrderService
        rpc: WatchOrder
        type: server-streaming
        scenarios:
          - name: ok
            response:
              status: OK
              messages: {event: CREATED}
`, "'messages'"},
		{"websocket messages non-list", `
openmock: 0.2.0
servers:
  - name: main
    type: websocket
    operations:
      - path: /ws
        scenarios:
          - name: ok
            response:
              messages: hi
`, "'messages'"},
		{"websocket empty messages ok", `
openmock: 0.2.0
servers:
  - name: main
    type: websocket
    operations:
      - path: /ws
        scenarios:
          - name: ok
            response:
              messages: []
`, ""},

		// --- GraphQL errors: $defs/graphqlError ---
		{"errors non-list", graphqlDocErrors(`{message: boom}`), "'errors'"},
		{"errors empty list", graphqlDocErrors(`[]`), "'errors'"},
		{"error missing message", graphqlDocErrors(`
                - path: [order]
`), "'message'"},
		{"error message not a string", graphqlDocErrors(`
                - message: 123
`), "'message'"},
		{"error unknown member", graphqlDocErrors(`
                - message: boom
                  code: NOT_FOUND
`), "unknown field"},
		{"error x- member", graphqlDocErrors(`
                - message: boom
                  x-hint: retry
`), "unknown field"},
		{"location line zero", graphqlDocErrors(`
                - message: boom
                  locations:
                    - {line: 0, column: 1}
`), "'line'"},
		{"location missing column", graphqlDocErrors(`
                - message: boom
                  locations:
                    - {line: 1}
`), "'column'"},
		{"location extra member", graphqlDocErrors(`
                - message: boom
                  locations:
                    - {line: 1, column: 1, file: a.graphql}
`), "unknown field"},
		{"path negative integer", graphqlDocErrors(`
                - message: boom
                  path: [order, -1]
`), "path"},
		{"path fractional number", graphqlDocErrors(`
                - message: boom
                  path: [order, 1.5]
`), "path"},
		{"path mapping element", graphqlDocErrors(`
                - message: boom
                  path: [{}]
`), "path"},
		{"extensions non-mapping", graphqlDocErrors(`
                - message: boom
                  extensions: oops
`), "'extensions'"},
		{"full valid error ok", graphqlDocErrors(`
                - message: order not found
                  locations:
                    - {line: 2, column: 7}
                  path: [order, 0]
                  extensions:
                    code: NOT_FOUND
`), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.Parse([]byte(tc.yaml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse rejected a valid document: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse accepted an invalid document (want error containing %q)", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// httpDoc returns a minimal valid HTTP document with extra server-level
// lines injected.
func httpDoc(serverField string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: http
    ` + serverField + `
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response:
              status: 200
`
}

func httpDocStatus(status string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response:
              status: ` + status + `
`
}

func httpDocMethod(method string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: ` + method + `
        path: /ping
        scenarios:
          - name: ok
            response:
              status: 200
`
}

func httpDocWhen(when string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: cond
            when: ` + when + `
            response:
              status: 200
          - name: ok
            response:
              status: 200
`
}

func wsDocClose(code string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: websocket
    operations:
      - path: /ws
        scenarios:
          - name: ok
            response:
              messages: []
              close:
                code: ` + code + `
`
}

// graphqlDocErrors returns a GraphQL document whose response carries the
// given errors value (either inline flow or an indented block starting on
// the next line).
func graphqlDocErrors(errors string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: graphql
    operations:
      - operationName: GetOrder
        scenarios:
          - name: err
            response:
              data:
                order: null
              errors: ` + errors + `
`
}

func grpcDoc(serviceLine, rpcLine string) string {
	return `
openmock: 0.2.0
servers:
  - name: main
    type: grpc
    operations:
      - ` + serviceLine + `
        ` + rpcLine + `
        type: unary
        scenarios:
          - name: ok
            response:
              status: OK
              message: {ok: true}
`
}
