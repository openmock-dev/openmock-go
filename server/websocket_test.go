package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

const wsDoc = `
openmock: 0.2.0
servers:
  - name: feed
    type: websocket
    operations:
      - path: /ws/orders/{id}
        scenarios:
          - name: welcome
            when:
              calls: 1
            response:
              messages:
                - type: welcome
                  orderId: "{{params.id}}"
          - name: heartbeat
            when:
              message:
                type: ping
            response:
              messages: []
          - name: status
            when:
              message:
                type: status
            response:
              messages:
                - type: status
                  orderId: "{{params.id}}"
                  state: SHIPPED
          - name: bye
            when:
              message:
                type: bye
            response:
              messages:
                - type: goodbye
              close:
                code: 1000
                reason: client requested close
          - name: unknown
            response:
              messages:
                - type: error
                  received: "{{request.message.type}}"
`

func wsServer(t *testing.T, doc string) string {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	port := freePort(t)
	srv, err := server.Serve(d, server.Options{AdminPortEphemeral: true, Ports: map[string]int{"feed": port}})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Wait()
	})
	return fmt.Sprintf("ws://127.0.0.1:%d", port)
}

func wsDial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func wsSend(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func wsRecv(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode reply %q: %v", data, err)
	}
	return m
}

func TestWebSocketExchange(t *testing.T) {
	base := wsServer(t, wsDoc)
	c := wsDial(t, base+"/ws/orders/7")

	// First message on the connection -> welcome (calls:1), with the path
	// param templated.
	wsSend(t, c, map[string]any{"type": "status"})
	if got := wsRecv(t, c); got["type"] != "welcome" || got["orderId"] != "7" {
		t.Fatalf("first reply = %v, want welcome/orderId 7", got)
	}

	// A status message -> status reply.
	wsSend(t, c, map[string]any{"type": "status"})
	if got := wsRecv(t, c); got["type"] != "status" || got["state"] != "SHIPPED" {
		t.Fatalf("status reply = %v, want status/SHIPPED", got)
	}

	// A ping is ignored (empty messages, connection stays open): the next
	// reply we see must be for the status that follows it.
	wsSend(t, c, map[string]any{"type": "ping"})
	wsSend(t, c, map[string]any{"type": "status"})
	if got := wsRecv(t, c); got["type"] != "status" {
		t.Fatalf("after ping, next reply = %v, want the following status (ping must reply nothing)", got)
	}
}

// TestWebSocketClose pins a reply-then-close response (spec §7.5): the client
// gets the goodbye message and then the connection closes with the declared
// code/reason.
func TestWebSocketClose(t *testing.T) {
	base := wsServer(t, wsDoc)
	c := wsDial(t, base+"/ws/orders/7")

	wsSend(t, c, map[string]any{"type": "bye"}) // first call is welcome...
	if got := wsRecv(t, c); got["type"] != "welcome" {
		t.Fatalf("first reply = %v, want welcome", got)
	}
	wsSend(t, c, map[string]any{"type": "bye"})
	if got := wsRecv(t, c); got["type"] != "goodbye" {
		t.Fatalf("reply = %v, want goodbye", got)
	}
	// The next read observes the close.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatalf("expected the connection to be closed after goodbye")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure {
		t.Errorf("close status = %v, want 1000", status)
	}
}

// TestWebSocketPerConnectionCounter pins spec §6.3: the call counter is keyed
// per connection, so each new connection's scripted exchange starts at 1
// (welcome again).
func TestWebSocketPerConnectionCounter(t *testing.T) {
	base := wsServer(t, wsDoc)

	for i := 0; i < 2; i++ {
		c := wsDial(t, base+"/ws/orders/7")
		wsSend(t, c, map[string]any{"type": "status"})
		if got := wsRecv(t, c); got["type"] != "welcome" {
			t.Fatalf("connection %d first reply = %v, want welcome (counter resets per connection)", i, got)
		}
		c.Close(websocket.StatusNormalClosure, "")
	}
}

// TestWebSocketUnroutedRefused pins spec §5.9/§10.2: a path matching no
// operation is refused at establishment — the upgrade does not complete and a
// 404 with the unrouted synthetic is returned, rather than accepting the
// connection.
func TestWebSocketUnroutedRefused(t *testing.T) {
	base := wsServer(t, wsDoc)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, base+"/ws/unknown", nil)
	if err == nil {
		c.CloseNow()
		t.Fatalf("Dial to an unrouted path succeeded, want refusal")
	}
	if resp == nil || resp.StatusCode != 404 {
		t.Fatalf("refusal status = %v, want 404", resp)
	}
}

// TestWebSocketDefaultScenario pins the default reply for an unmatched message
// type.
func TestWebSocketDefaultScenario(t *testing.T) {
	base := wsServer(t, wsDoc)
	c := wsDial(t, base+"/ws/orders/1")
	wsSend(t, c, map[string]any{"type": "status"}) // welcome (call 1)
	wsRecv(t, c)
	wsSend(t, c, map[string]any{"type": "frobnicate"})
	if got := wsRecv(t, c); got["type"] != "error" || got["received"] != "frobnicate" {
		t.Errorf("default reply = %v, want error/received frobnicate", got)
	}
}

func TestWebSocketTextScalarMessage(t *testing.T) {
	// A plain-text (non-JSON) frame matches the scalar message form.
	doc := `
openmock: 0.2.0
servers:
  - name: feed
    type: websocket
    operations:
      - path: /ws
        scenarios:
          - name: pong
            when:
              message: ping
            response:
              messages:
                - "{{request.message}}-pong"
`
	base := wsServer(t, doc)
	c := wsDial(t, base+"/ws")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	_, data, err := c.Read(rctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(data); !strings.EqualFold(got, "ping-pong") {
		t.Errorf("scalar reply = %q, want ping-pong", got)
	}
}
