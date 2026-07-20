package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
)

// Handler is the HTTP protocol adapter (milestone 2): an http.Handler bound
// to one document server that maps a real HTTP request onto the engine's
// normalized request model and the engine's response back onto the wire. It
// is a thin layer — all matching, templating, and state live in the engine.
//
// One Handler serves one named server of a document; a serving process
// mounts a Handler per HTTP server, all sharing a single *engine.Engine so
// the call counters (keyed by server name) stay coherent.
type Handler struct {
	engine     *engine.Engine
	serverName string
}

// NewHandler returns an HTTP adapter that resolves requests as the named
// server of eng's document.
func NewHandler(eng *engine.Engine, serverName string) *Handler {
	return &Handler{engine: eng, serverName: serverName}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := h.buildRequest(r)
	resp := h.engine.Resolve(req)
	writeResponse(w, resp)
}

// buildRequest maps an incoming *http.Request onto engine.Request. It does
// the normalization the engine's JSON decoder would otherwise do (the engine
// does none of it for a struct built by field): method uppercased, header
// names lowercased and repeats joined with ", " (spec §4.6), and — crucially
// — the path taken RAW from EscapedPath so percent-encoding is preserved for
// the engine's own per-segment decode (spec §5.2). r.URL.Path is already
// decoded and would collapse an encoded %2F into a segment separator.
func (h *Handler) buildRequest(r *http.Request) *engine.Request {
	req := &engine.Request{
		Protocol: "http",
		Server:   h.serverName,
		Method:   strings.ToUpper(r.Method),
		Path:     r.URL.EscapedPath(),
		Query:    map[string][]string(r.URL.Query()),
		Headers:  normalizeHeaders(r.Header),
		Body:     parseBody(r),
	}
	return req
}

// normalizeHeaders lowercases header names and joins repeated field lines
// with ", " (spec §4.6), matching how the engine expects Headers.
func normalizeHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for name, values := range h {
		out[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	return out
}

// parseBody reads the request body into the engine's `any` model: JSON when
// the Content-Type says so (an object/array/scalar), otherwise the raw text
// as a string scalar (matches a `text/plain` body against a scalar `body:`
// facet, corpus case 42). An absent or empty body is nil.
func parseBody(r *http.Request) any {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		return nil
	}
	if isJSONContentType(r.Header.Get("Content-Type")) {
		var v any
		if json.Unmarshal(data, &v) == nil {
			return v
		}
		// Malformed JSON falls back to the raw text rather than failing the
		// request; the engine treats it as a scalar payload.
	}
	return string(data)
}

func isJSONContentType(ct string) bool {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	return ct == "application/json" || strings.HasSuffix(ct, "+json")
}

// writeResponse renders the engine's normalized HTTP response
// (map[string]any) onto the wire: optional delay, then headers (a list value
// emits one field line per item, e.g. Set-Cookie), then status, then body.
func writeResponse(w http.ResponseWriter, resp map[string]any) {
	if d, ok := resp["delay"].(float64); ok && d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}

	if headers, ok := resp["headers"].(map[string]any); ok {
		for name, value := range headers {
			switch v := value.(type) {
			case string:
				w.Header().Set(name, v)
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						w.Header().Add(name, s)
					}
				}
			}
		}
	}

	body, hasBody := resp["body"]
	if hasBody && body != nil {
		setDefaultContentType(w.Header(), body)
	}

	status := http.StatusOK
	if s, ok := resp["status"].(float64); ok {
		status = int(s)
	}
	w.WriteHeader(status)

	if hasBody && body != nil {
		writeBody(w, body)
	}
}

// setDefaultContentType supplies a Content-Type only when the document did
// not declare one: JSON for a structured/numeric/boolean body, text for a
// string body. Serving is non-normative (docs/serving.md), so this is an
// adapter policy, not a spec requirement.
func setDefaultContentType(h http.Header, body any) {
	if h.Get("Content-Type") != "" {
		return
	}
	if _, isString := body.(string); isString {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		h.Set("Content-Type", "application/json")
	}
}

// writeBody emits a string body as raw text and any other body as JSON.
func writeBody(w http.ResponseWriter, body any) {
	if s, ok := body.(string); ok {
		io.WriteString(w, s)
		return
	}
	data, err := json.Marshal(body)
	if err != nil {
		return
	}
	w.Write(data)
}
