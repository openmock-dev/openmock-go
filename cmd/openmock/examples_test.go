package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

// TestExamplesParse keeps the shipped examples honest: every examples/*.yml
// must load through the engine, so a broken or stale example fails CI rather
// than only surfacing when someone runs `openmock serve` on it.
func TestExamplesParse(t *testing.T) {
	paths, err := filepath.Glob("../../examples/*.yml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no examples found under ../../examples")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if _, err := engine.Parse(data); err != nil {
				t.Errorf("%s does not parse: %v", p, err)
			}
		})
	}
}
