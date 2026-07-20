package main

import "testing"

func TestParseServeArgs(t *testing.T) {
	t.Setenv("OPENMOCK_PORTS", "")

	f, err := parseServeArgs([]string{"mocks.yml", "--bind", "0.0.0.0", "--port", "a=3000", "--port", "b=3001", "--admin-port", "9000"})
	if err != nil {
		t.Fatalf("parseServeArgs: %v", err)
	}
	if f.file != "mocks.yml" {
		t.Errorf("file = %q, want mocks.yml", f.file)
	}
	if f.bind != "0.0.0.0" {
		t.Errorf("bind = %q, want 0.0.0.0", f.bind)
	}
	if f.ports["a"] != 3000 || f.ports["b"] != 3001 {
		t.Errorf("ports = %v, want a=3000 b=3001", f.ports)
	}
	if f.adminPort != 9000 || f.adminEph {
		t.Errorf("admin = %d eph=%v, want 9000 non-ephemeral", f.adminPort, f.adminEph)
	}
}

func TestParseServeArgsInlineAndEphemeralAdmin(t *testing.T) {
	t.Setenv("OPENMOCK_PORTS", "")
	f, err := parseServeArgs([]string{"--bind=127.0.0.1", "--admin-port=0", "mocks.yml"})
	if err != nil {
		t.Fatalf("parseServeArgs: %v", err)
	}
	if f.bind != "127.0.0.1" || f.file != "mocks.yml" {
		t.Errorf("bind=%q file=%q, want 127.0.0.1 / mocks.yml", f.bind, f.file)
	}
	if !f.adminEph {
		t.Errorf("adminEph = false, want true for --admin-port=0")
	}
}

// TestServeArgsFlagOverridesEnv pins that OPENMOCK_PORTS seeds the ports but a
// --port flag overrides the same name.
func TestServeArgsFlagOverridesEnv(t *testing.T) {
	t.Setenv("OPENMOCK_PORTS", "a=1000,b=2000")
	f, err := parseServeArgs([]string{"mocks.yml", "--port", "a=3000"})
	if err != nil {
		t.Fatalf("parseServeArgs: %v", err)
	}
	if f.ports["a"] != 3000 {
		t.Errorf("ports[a] = %d, want 3000 (flag overrides env)", f.ports["a"])
	}
	if f.ports["b"] != 2000 {
		t.Errorf("ports[b] = %d, want 2000 (from env)", f.ports["b"])
	}
}

func TestParseServeArgsErrors(t *testing.T) {
	t.Setenv("OPENMOCK_PORTS", "")
	cases := [][]string{
		{},                              // no file
		{"--bind"},                      // flag needs a value
		{"mocks.yml", "--port", "oops"}, // bad port mapping
		{"mocks.yml", "--admin-port", "-1"},
		{"mocks.yml", "--unknown"},
	}
	for _, args := range cases {
		if _, err := parseServeArgs(args); err == nil {
			t.Errorf("parseServeArgs(%v) = nil error, want failure", args)
		}
	}
}
