// Package engine_test runs the OpenMock conformance corpus (vendored under
// testdata/conformance from openmock-dev/openmock) against this engine, per
// spec §12 and conformance/README.md. This is the normative check for
// milestone 1 of openmock-dev/openmock's reference-tooling roadmap: a Go
// engine is conformant when it produces every case's expected outcome.
package engine_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

const corpusRoot = "testdata/conformance"

func TestConformance(t *testing.T) {
	entries, err := os.ReadDir(corpusRoot)
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(corpusRoot, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			runCase(t, dir)
		})
	}
}

func runCase(t *testing.T, dir string) {
	data, err := os.ReadFile(filepath.Join(dir, "input.yml"))
	if err != nil {
		t.Fatalf("reading input.yml: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "invalid.json")); err == nil {
		if doc, err := engine.Parse(data); err == nil {
			t.Fatalf("Parse unexpectedly accepted an invalid document: %+v", doc)
		}
		return
	}

	doc, err := engine.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "steps.json")); err == nil {
		runSteps(t, dir, doc)
		return
	}
	runSingleShot(t, dir, doc)
}

func runSingleShot(t *testing.T, dir string, doc *engine.Document) {
	reqData, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatalf("reading request.json: %v", err)
	}
	var req engine.Request
	if err := json.Unmarshal(reqData, &req); err != nil {
		t.Fatalf("decoding request.json: %v", err)
	}

	expData, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatalf("reading expected.json: %v", err)
	}
	var expected map[string]any
	if err := json.Unmarshal(expData, &expected); err != nil {
		t.Fatalf("decoding expected.json: %v", err)
	}

	actual := engine.New(doc).Resolve(&req)
	assertResponse(t, expected, actual)
}

func runSteps(t *testing.T, dir string, doc *engine.Document) {
	data, err := os.ReadFile(filepath.Join(dir, "steps.json"))
	if err != nil {
		t.Fatalf("reading steps.json: %v", err)
	}
	var steps []struct {
		Request  engine.Request `json:"request"`
		Expected map[string]any `json:"expected"`
	}
	if err := json.Unmarshal(data, &steps); err != nil {
		t.Fatalf("decoding steps.json: %v", err)
	}

	eng := engine.New(doc)
	for i, step := range steps {
		actual := eng.Resolve(&step.Request)
		t.Run(fmt.Sprintf("step-%d", i+1), func(t *testing.T) {
			assertResponse(t, step.Expected, actual)
		})
	}
}

// assertResponse implements conformance/README.md's assertion semantics: a
// field absent from expected is not asserted; status/delay/close/error
// strings and every payload field assert exact deep equality; headers,
// metadata, and trailers assert a subset (every expected key/value present,
// extra actual entries allowed).
func assertResponse(t *testing.T, expected, actual map[string]any) {
	t.Helper()
	for k, ev := range expected {
		av, present := actual[k]
		switch k {
		case "headers", "metadata", "trailers":
			evm, _ := ev.(map[string]any)
			avm, ok := av.(map[string]any)
			if !present || !ok {
				t.Errorf("%s: expected a map containing %v, actual response has %#v", k, evm, av)
				continue
			}
			assertMapSubset(t, k, evm, avm)
		default:
			if !present {
				t.Errorf("field %q: expected %s, but the response omitted it", k, jsonString(ev))
				continue
			}
			if !deepEqualJSON(ev, av) {
				t.Errorf("field %q mismatch:\n  expected: %s\n  actual:   %s", k, jsonString(ev), jsonString(av))
			}
		}
	}
}

func assertMapSubset(t *testing.T, field string, expected, actual map[string]any) {
	t.Helper()
	lower := make(map[string]any, len(actual))
	for k, v := range actual {
		lower[strings.ToLower(k)] = v
	}
	for k, ev := range expected {
		av, ok := lower[strings.ToLower(k)]
		if !ok || !deepEqualJSON(ev, av) {
			t.Errorf("%s[%q]: expected %s, actual %s (full actual: %s)", field, k, jsonString(ev), jsonString(av), jsonString(actual))
		}
	}
}

func deepEqualJSON(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bvv, ok := bv[k]
			if !ok || !deepEqualJSON(v, bvv) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepEqualJSON(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(b)
}
