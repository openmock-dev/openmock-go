package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/openmock-dev/openmock-go/engine"
)

// wsHandler is the WebSocket adapter: an http.Handler bound to one document
// server that upgrades a connection and resolves each inbound message against
// the engine. WebSocket is message-oriented — the connection carries fixed
// path/query/headers and every client message is one normalized request
// (spec §4.4) — and client-initiated only in v0.2: the mock speaks only in
// reply.
type wsHandler struct {
	engine     *engine.Engine
	serverName string
}

func newWSHandler(eng *engine.Engine, serverName string) *wsHandler {
	return &wsHandler{engine: eng, serverName: serverName}
}

// wsConnSeq gives each accepted connection a unique id, which keys its own
// per-connection call counter in the engine (spec §6.3): every connection's
// scripted exchange starts from call 1.
var wsConnSeq atomic.Uint64

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath() // raw, for the engine's per-segment decode (§5.2)

	// Routing happens once, at establishment (spec §5.9). A path that matches
	// no operation is refused with the 404 unrouted synthetic instead of
	// completing the upgrade (spec §10.2) — never accepted then closed. Routes
	// has no side effects, so this consumes no call.
	probe := &engine.Request{Protocol: "websocket", Server: h.serverName, Path: path}
	if !h.engine.Routes(probe) {
		resp := h.engine.Resolve(probe) // unrouted → synthetic, no counter advance
		status := http.StatusNotFound
		if s, ok := resp["status"].(float64); ok {
			status = int(s)
		}
		writeJSON(w, status, resp["body"])
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// A mock server is a dev tool talking to arbitrary clients; do not
		// reject on Origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	// Connection-scoped values, fixed at establishment (spec §4.4).
	conn := strconv.FormatUint(wsConnSeq.Add(1), 10)
	query := map[string][]string(r.URL.Query())
	headers := normalizeHeaders(r.Header)

	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return // client closed, or the connection failed
		}

		resp := h.engine.Resolve(&engine.Request{
			Protocol:   "websocket",
			Server:     h.serverName,
			Path:       path,
			Query:      query,
			Headers:    headers,
			Connection: conn,
			Message:    parseWSMessage(typ, data),
		})
		if h.writeReply(ctx, c, resp) {
			return // the response closed the connection
		}
	}
}

// parseWSMessage normalizes an inbound frame to the engine's message model: a
// JSON text frame becomes its parsed value, and any other frame (or invalid
// JSON) becomes a string scalar (spec §4.4).
func parseWSMessage(typ websocket.MessageType, data []byte) any {
	if typ == websocket.MessageText {
		var v any
		if json.Unmarshal(data, &v) == nil {
			return v
		}
		return string(data)
	}
	// A binary frame has no scalar text form; hand it over as a string of its
	// bytes so scalar matching still has something to compare.
	return string(data)
}

// writeReply emits a resolved WebSocket response: an optional delay, then each
// reply message in order, then an optional close. It reports whether the
// connection was closed. An empty messages list with no close deliberately
// replies with nothing and keeps the connection open (spec §7.5).
func (h *wsHandler) writeReply(ctx context.Context, c *websocket.Conn, resp map[string]any) (closed bool) {
	if d, ok := resp["delay"].(float64); ok && d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	if msgs, ok := resp["messages"].([]any); ok {
		for _, m := range msgs {
			if err := c.Write(ctx, websocket.MessageText, encodeWSMessage(m)); err != nil {
				return true
			}
		}
	}
	if cl, ok := resp["close"].(map[string]any); ok {
		code := 1000
		if cf, ok := cl["code"].(float64); ok {
			code = int(cf)
		}
		reason, _ := cl["reason"].(string)
		c.Close(websocket.StatusCode(code), reason)
		return true
	}
	return false
}

// encodeWSMessage serializes a reply message to a text frame: a string is sent
// raw, and any other value as JSON (mirroring the HTTP body policy).
func encodeWSMessage(m any) []byte {
	if s, ok := m.(string); ok {
		return []byte(s)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return data
}
