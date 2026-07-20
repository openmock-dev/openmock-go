package server

import (
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

func parseInternal(t *testing.T, doc string) *engine.Document {
	t.Helper()
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

// TestResolvePortsRungs pins the four-rung resolution order of
// docs/serving.md §2 without binding any socket.
func TestResolvePortsRungs(t *testing.T) {
	soleHTTP := `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /p
        scenarios: [{name: ok, response: {status: 200}}]
`
	// Rung 3: exactly one HTTP server, no hint/config → stable default 3000.
	got, err := resolvePorts(parseInternal(t, soleHTTP), nil, defaultAdminPort)
	if err != nil {
		t.Fatalf("resolvePorts: %v", err)
	}
	if len(got) != 1 || got[0].port != defaultHTTPPort || got[0].source != sourceDefault {
		t.Fatalf("sole server = %+v, want port 3000 from sourceDefault", got)
	}

	// Rung 2: a document hint beats the default.
	hinted := `
openmock: 0.2.0
servers:
  - name: main
    type: http
    port: 8080
    operations:
      - method: GET
        path: /p
        scenarios: [{name: ok, response: {status: 200}}]
`
	got, _ = resolvePorts(parseInternal(t, hinted), nil, defaultAdminPort)
	if got[0].port != 8080 || got[0].source != sourceHint {
		t.Fatalf("hinted = %+v, want 8080 from sourceHint", got)
	}

	// Rung 1: explicit config beats the hint.
	got, _ = resolvePorts(parseInternal(t, hinted), map[string]int{"main": 9090}, defaultAdminPort)
	if got[0].port != 9090 || got[0].source != sourceConfig {
		t.Fatalf("configured = %+v, want 9090 from sourceConfig", got)
	}
}

// TestResolvePortsTwoOfATypeNoDefault pins that the sole-server default only
// applies to exactly one server of a type: two HTTP servers fall through to
// the ephemeral rung (port 0), never to a reserved default.
func TestResolvePortsTwoOfATypeNoDefault(t *testing.T) {
	doc := `
openmock: 0.2.0
servers:
  - name: a
    type: http
    operations: [{method: GET, path: /p, scenarios: [{name: ok, response: {status: 200}}]}]
  - name: b
    type: http
    operations: [{method: GET, path: /p, scenarios: [{name: ok, response: {status: 200}}]}]
`
	got, err := resolvePorts(parseInternal(t, doc), nil, defaultAdminPort)
	if err != nil {
		t.Fatalf("resolvePorts: %v", err)
	}
	for _, rp := range got {
		if rp.port != 0 || rp.source != sourceEphemeral {
			t.Errorf("server %q = port %d/%v, want ephemeral (0)", rp.name, rp.port, rp.source)
		}
	}
}
