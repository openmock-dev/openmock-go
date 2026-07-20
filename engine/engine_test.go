package engine_test

import (
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

func mustParse(t *testing.T, doc string) *engine.Document {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func TestResolveDoesNotMutateRequest(t *testing.T) {
	d := mustParse(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /users/{id}
        scenarios:
          - name: ok
            response:
              status: 200
              body:
                id: "{{params.id}}"
`)
	req := &engine.Request{Protocol: "http", Method: "GET", Path: "/users/42"}
	resp := engine.New(d).Resolve(req)

	if req.Params != nil {
		t.Errorf("Resolve mutated the caller's request: req.Params = %v", req.Params)
	}
	body, _ := resp["body"].(map[string]any)
	if body["id"] != "42" {
		t.Errorf("path-param templating broke: body = %v", resp["body"])
	}
}

func TestReset(t *testing.T) {
	d := mustParse(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: first
            when:
              calls: 1
            response:
              status: 201
          - name: rest
            response:
              status: 200
`)
	eng := engine.New(d)
	req := func() *engine.Request {
		return &engine.Request{Protocol: "http", Method: "GET", Path: "/ping"}
	}

	if s := eng.Resolve(req())["status"]; s != float64(201) {
		t.Fatalf("1st call: status = %v, want 201", s)
	}
	if s := eng.Resolve(req())["status"]; s != float64(200) {
		t.Fatalf("2nd call: status = %v, want 200", s)
	}
	eng.Reset()
	if s := eng.Resolve(req())["status"]; s != float64(201) {
		t.Fatalf("1st call after Reset: status = %v, want 201", s)
	}
}

// TestCounterKeyNoCollision pins that counter identity is per key slot, not
// per joined string: under a naive "name|path|connection" join, the
// WebSocket tuples (path "/p", connection "c|z") and (path "/p|c",
// connection "z") produce the identical key "w|/p|c|z" and share one
// counter. Each connection below is the first message on its own
// (operation, connection) counter (spec §6.3), so both must see calls==1.
func TestCounterKeyNoCollision(t *testing.T) {
	d := mustParse(t, `
openmock: 0.2.0
servers:
  - name: w
    type: websocket
    operations:
      - path: /p
        scenarios:
          - name: first
            when:
              calls: 1
            response:
              messages:
                - hello-first
          - name: rest
            response:
              messages:
                - hello-rest
      - path: "/p|c"
        scenarios:
          - name: first
            when:
              calls: 1
            response:
              messages:
                - bar-first
          - name: rest
            response:
              messages:
                - bar-rest
`)
	eng := engine.New(d)

	respA := eng.Resolve(&engine.Request{
		Protocol: "websocket", Server: "w", Path: "/p", Connection: "c|z",
		Message: "ping",
	})
	if msgs, _ := respA["messages"].([]any); len(msgs) != 1 || msgs[0] != "hello-first" {
		t.Fatalf("connection A, 1st message: messages = %v, want [hello-first]", respA["messages"])
	}
	respB := eng.Resolve(&engine.Request{
		Protocol: "websocket", Server: "w", Path: "/p|c", Connection: "z",
		Message: "ping",
	})
	if msgs, _ := respB["messages"].([]any); len(msgs) != 1 || msgs[0] != "bar-first" {
		t.Fatalf("connection B, 1st message: messages = %v, want [bar-first]", respB["messages"])
	}
}
