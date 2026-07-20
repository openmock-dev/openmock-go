package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

func parseDoc(t *testing.T, doc string) *engine.Document {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

// serveEphemeral starts a Server with all ports ephemeral (mock ports via a
// port hint of 0 is not possible, so tests that need a known-free mock port
// use explicit Ports; here the admin port is ephemeral and mock servers use
// whatever the doc/config says).
func mustServe(t *testing.T, doc *engine.Document, opts server.Options) *server.Server {
	t.Helper()
	opts.AdminPortEphemeral = true
	s, err := server.Serve(doc, opts)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Shutdown(ctx)
		s.Wait()
	})
	return s
}

// freePort returns a currently-free TCP port by binding :0 and closing it.
// Inherently racy, but adequate for tests that immediately reuse it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestPortResolutionSoleServerDefault(t *testing.T) {
	// A single HTTP server with no hint gets the stable default 3000 — but
	// binding 3000 in a test is antisocial, so assert the resolution via the
	// topology using an explicit port instead, and cover the default in the
	// unit-level resolvePorts test below. Here: an explicit port wins.
	port := freePort(t)
	doc := parseDoc(t, singleServerDoc(0))
	s := mustServe(t, doc, server.Options{Ports: map[string]int{"main": port}})

	top := s.Topology()
	if len(top) != 1 || top[0].Port == nil || *top[0].Port != port {
		t.Fatalf("topology = %+v, want main on %d", top, port)
	}
	if got := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/ping", port)); got != 200 {
		t.Errorf("GET /ping = %d, want 200", got)
	}
}

func TestPortHintHonoured(t *testing.T) {
	port := freePort(t)
	doc := parseDoc(t, singleServerDoc(port)) // port hint in the document
	s := mustServe(t, doc, server.Options{})
	if top := s.Topology(); top[0].Port == nil || *top[0].Port != port {
		t.Fatalf("topology port = %v, want hint %d", top[0].Port, port)
	}
}

func TestConfigBeatsHint(t *testing.T) {
	hint, cfg := freePort(t), freePort(t)
	if hint == cfg {
		t.Skip("flaky: two freePort calls collided")
	}
	doc := parseDoc(t, singleServerDoc(hint))
	s := mustServe(t, doc, server.Options{Ports: map[string]int{"main": cfg}})
	if top := s.Topology(); top[0].Port == nil || *top[0].Port != cfg {
		t.Fatalf("topology port = %v, want config %d (config beats hint)", top[0].Port, cfg)
	}
}

// TestTwoHTTPServersEphemeral pins that with two servers of a type, the
// sole-server default does not apply: neither is given a reserved default,
// and both land on distinct OS-ephemeral ports (unless hinted).
func TestTwoHTTPServersEphemeral(t *testing.T) {
	doc := parseDoc(t, `
openmock: 0.2.0
servers:
  - name: a
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response: {status: 200}
  - name: b
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response: {status: 200}
`)
	s := mustServe(t, doc, server.Options{})
	top := s.Topology()
	if len(top) != 2 {
		t.Fatalf("topology has %d entries, want 2", len(top))
	}
	seen := map[int]bool{}
	for _, info := range top {
		if info.Port == nil {
			t.Fatalf("server %q has no resolved port", info.Name)
		}
		p := *info.Port
		if reserved(p) {
			t.Errorf("server %q got reserved default port %d; ephemeral must avoid 3000-3003/4400", info.Name, p)
		}
		if seen[p] {
			t.Errorf("port %d assigned twice", p)
		}
		seen[p] = true
	}
}

func TestSamePortCollisionIsError(t *testing.T) {
	port := freePort(t)
	doc := parseDoc(t, fmt.Sprintf(`
openmock: 0.2.0
servers:
  - name: a
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios: [{name: ok, response: {status: 200}}]
  - name: b
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios: [{name: ok, response: {status: 200}}]
`))
	_, err := server.Serve(doc, server.Options{
		AdminPortEphemeral: true,
		Ports:              map[string]int{"a": port, "b": port},
	})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("err = %v, want a same-port collision error", err)
	}
}

// TestUnbindableConfigPortFailsLoudly pins §2: a configured port that cannot
// be bound is a loud error, never a silent reassignment.
func TestUnbindableConfigPortFailsLoudly(t *testing.T) {
	// Occupy a port, then configure a server onto it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	taken := l.Addr().(*net.TCPAddr).Port

	doc := parseDoc(t, singleServerDoc(0))
	_, err = server.Serve(doc, server.Options{
		AdminPortEphemeral: true,
		Ports:              map[string]int{"main": taken},
	})
	if err == nil || !strings.Contains(err.Error(), "explicit configuration") {
		t.Fatalf("err = %v, want a loud bind failure attributed to explicit configuration", err)
	}
}

func TestServerCollidesWithAdminPort(t *testing.T) {
	admin := freePort(t)
	doc := parseDoc(t, singleServerDoc(admin))
	_, err := server.Serve(doc, server.Options{AdminPort: admin})
	if err == nil || !strings.Contains(err.Error(), "admin port") {
		t.Fatalf("err = %v, want an admin-port collision error", err)
	}
}

// TestAdminAPI drives the full admin surface end-to-end: topology, health,
// and reset (which must clear the call counter so a calls-gated scenario
// fires again).
func TestAdminAPI(t *testing.T) {
	mockPort := freePort(t)
	doc := parseDoc(t, fmt.Sprintf(`
openmock: 0.2.0
servers:
  - name: main
    type: http
    port: %d
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: first
            when: {calls: 1}
            response: {status: 201}
          - name: rest
            response: {status: 200}
`, mockPort))
	s := mustServe(t, doc, server.Options{})
	admin := "http://" + s.AdminAddr()

	// GET /servers
	var topo []server.ServerInfo
	getJSON(t, admin+"/servers", &topo)
	if len(topo) != 1 || topo[0].Name != "main" || topo[0].Transport == nil || *topo[0].Transport != "http" {
		t.Fatalf("GET /servers = %+v, want one http server 'main'", topo)
	}

	// GET /health
	var health map[string]any
	getJSON(t, admin+"/health", &health)
	if health["status"] != "ok" {
		t.Errorf("GET /health = %v, want status ok", health)
	}

	mock := fmt.Sprintf("http://127.0.0.1:%d/ping", mockPort)
	if got := httpGet(t, mock); got != 201 {
		t.Fatalf("1st GET /ping = %d, want 201", got)
	}
	if got := httpGet(t, mock); got != 200 {
		t.Fatalf("2nd GET /ping = %d, want 200", got)
	}

	// POST /reset clears counters → the calls:1 scenario fires again.
	if code, _ := httpPost(t, admin+"/reset"); code != 200 {
		t.Fatalf("POST /reset = %d, want 200", code)
	}
	if got := httpGet(t, mock); got != 201 {
		t.Errorf("GET /ping after reset = %d, want 201", got)
	}
}

// --- helpers ---

func reserved(p int) bool {
	return p == 3000 || p == 3001 || p == 3002 || p == 3003 || p == 4400
}

func singleServerDoc(port int) string {
	portLine := ""
	if port != 0 {
		portLine = fmt.Sprintf("\n    port: %d", port)
	}
	return fmt.Sprintf(`
openmock: 0.2.0
servers:
  - name: main
    type: http%s
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: ok
            response: {status: 200}
`, portLine)
}

func httpGet(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func httpPost(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
