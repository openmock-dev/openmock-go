package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

func handlerFor(t *testing.T, doc, serverName string) *server.Handler {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return server.NewHandler(engine.New(d), serverName)
}

func do(h *server.Handler, r *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Result()
}

func TestMatchedRequest(t *testing.T) {
	h := handlerFor(t, `
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
              headers:
                Content-Type: application/json
              body:
                id: "{{params.id}}"
                name: Ada
`, "main")

	resp := do(h, httptest.NewRequest("GET", "/users/42", nil))
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); !strings.Contains(got, `"id":"42"`) || !strings.Contains(got, `"name":"Ada"`) {
		t.Errorf("body = %s, want id 42 and name Ada", got)
	}
}

// TestRawPathPreserved pins that the adapter hands the engine the raw,
// percent-encoded path: %2F must not be pre-decoded into a segment
// separator, and {name} must capture the decoded value (spec §5.2, corpus
// case 36).
func TestRawPathPreserved(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /files/{name}
        scenarios:
          - name: ok
            response:
              status: 200
              body:
                name: "{{params.name}}"
`, "main")

	// EscapedPath keeps %2F encoded; the engine decodes it to a literal
	// slash inside the single captured segment.
	req := httptest.NewRequest("GET", "/files/a%2Fb.txt", nil)
	resp := do(h, req)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 (raw path should stay one segment)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); !strings.Contains(got, `"name":"a/b.txt"`) {
		t.Errorf("body = %s, want captured name a/b.txt", got)
	}
}

// TestListHeaderEmitsMultipleLines pins that a list-valued response header
// (Set-Cookie) is emitted as one field line per item (spec §4.6/§7, corpus
// case 39).
func TestListHeaderEmitsMultipleLines(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /login
        scenarios:
          - name: ok
            response:
              status: 200
              headers:
                Set-Cookie:
                  - "a=1; Path=/"
                  - "b=2; Path=/"
`, "main")

	resp := do(h, httptest.NewRequest("GET", "/login", nil))
	cookies := resp.Header["Set-Cookie"]
	if len(cookies) != 2 || cookies[0] != "a=1; Path=/" || cookies[1] != "b=2; Path=/" {
		t.Errorf("Set-Cookie = %v, want two ordered field lines", cookies)
	}
}

// TestTextBody pins that a text/plain request body is matched as a scalar
// payload and a scalar response body is emitted as raw text (corpus case 42).
func TestTextBody(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: POST
        path: /echo
        scenarios:
          - name: pong
            when:
              body: ping
            response:
              status: 200
              body: "{{request.body}}-pong"
`, "main")

	req := httptest.NewRequest("POST", "/echo", strings.NewReader("ping"))
	req.Header.Set("Content-Type", "text/plain")
	resp := do(h, req)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain default", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "ping-pong" {
		t.Errorf("body = %q, want ping-pong (raw text, not JSON-quoted)", got)
	}
}

func TestJSONBodyMatching(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: POST
        path: /users
        scenarios:
          - name: admin
            when:
              body:
                role: admin
            response:
              status: 201
              body:
                created: true
          - name: other
            response:
              status: 200
`, "main")

	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	if resp := do(h, req); resp.StatusCode != 201 {
		t.Errorf("admin body: status = %d, want 201", resp.StatusCode)
	}

	req2 := httptest.NewRequest("POST", "/users", strings.NewReader(`{"role":"user"}`))
	req2.Header.Set("Content-Type", "application/json")
	if resp := do(h, req2); resp.StatusCode != 200 {
		t.Errorf("non-admin body: status = %d, want 200", resp.StatusCode)
	}
}

func TestUnroutedAndUnmatched(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: only-when
            when:
              headers:
                X-Key: secret
            response:
              status: 200
`, "main")

	// Unknown route → 404 unrouted synthetic.
	resp := do(h, httptest.NewRequest("GET", "/nope", nil))
	if resp.StatusCode != 404 {
		t.Errorf("unrouted: status = %d, want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"openmock":"unrouted"`) {
		t.Errorf("unrouted body = %s, want openmock:unrouted", body)
	}

	// Routed but no scenario matches and no default → 501 unmatched.
	resp2 := do(h, httptest.NewRequest("GET", "/ping", nil))
	if resp2.StatusCode != 501 {
		t.Errorf("unmatched: status = %d, want 501", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), `"openmock":"unmatched"`) {
		t.Errorf("unmatched body = %s, want openmock:unmatched", body2)
	}
}

// TestRepeatedQueryAndHeaders pins that repeated query params reach the
// engine as an ordered list (matched exactly) while repeated headers are
// joined with ", " (spec §4.6).
func TestRepeatedQueryAndHeaders(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /search
        scenarios:
          - name: matched
            when:
              query:
                ids: ["1", "2"]
              headers:
                X-Tag: alpha, beta
            response:
              status: 200
          - name: fallback
            response:
              status: 418
`, "main")

	req := httptest.NewRequest("GET", "/search?ids=1&ids=2", nil)
	req.Header.Add("X-Tag", "alpha")
	req.Header.Add("X-Tag", "beta")
	if resp := do(h, req); resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200 (list query + joined headers should match)", resp.StatusCode)
	}
}

// TestDelayApplied pins that a declared delay is honoured before the
// response is written.
func TestDelayApplied(t *testing.T) {
	h := handlerFor(t, `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /slow
        scenarios:
          - name: ok
            response:
              status: 200
              delay: 40
`, "main")

	start := time.Now()
	do(h, httptest.NewRequest("GET", "/slow", nil))
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~40ms delay", elapsed)
	}
}
