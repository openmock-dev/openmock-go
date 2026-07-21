package engine_test

import (
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

// TestRoutes pins that Routes reports routability without side effects — in
// particular without advancing the call counter that Resolve reads.
func TestRoutes(t *testing.T) {
	doc := `
openmock: 0.2.0
servers:
  - name: feed
    type: websocket
    operations:
      - path: /ws/orders/{id}
        scenarios:
          - name: first
            when:
              calls: 1
            response:
              messages:
                - hello
          - name: rest
            response:
              messages:
                - again
`
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	eng := engine.New(d)

	if !eng.Routes(&engine.Request{Protocol: "websocket", Server: "feed", Path: "/ws/orders/7"}) {
		t.Errorf("Routes = false for a matching path, want true")
	}
	if eng.Routes(&engine.Request{Protocol: "websocket", Server: "feed", Path: "/ws/unknown"}) {
		t.Errorf("Routes = true for a non-matching path, want false")
	}

	// Routes must not consume a call: the first Resolve still sees calls==1.
	for i := 0; i < 3; i++ {
		eng.Routes(&engine.Request{Protocol: "websocket", Server: "feed", Path: "/ws/orders/7", Connection: "c1"})
	}
	resp := eng.Resolve(&engine.Request{Protocol: "websocket", Server: "feed", Path: "/ws/orders/7", Connection: "c1"})
	msgs, _ := resp["messages"].([]any)
	if len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("first Resolve after 3 Routes = %v, want [hello] (Routes must not advance the counter)", resp["messages"])
	}
}
