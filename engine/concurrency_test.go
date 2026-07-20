package engine_test

import (
	"sync"
	"testing"

	"github.com/openmock-dev/openmock-go/engine"
)

// TestResolveConcurrent exercises the call counter under concurrent load.
// Run with -race, it guards against two regressions in one shot: Go's fatal
// "concurrent map writes" panic, and a lost/duplicated counter value from a
// non-atomic increment-then-read.
//
// The document returns 201 only on the 1st call and 200 on every call after
// (spec §6.3). Firing N requests concurrently at one key, the counter must
// hand out each of 1..N exactly once, so exactly one request may see calls==1
// — i.e. exactly one 201. Any race that double-counts or drops an increment
// would yield zero or several 201s.
func TestResolveConcurrent(t *testing.T) {
	const doc = `
openmock: 0.2.0
servers:
  - name: main
    type: http
    operations:
      - method: GET
        path: /ping
        scenarios:
          - name: first
            when:
              calls: 1
            response:
              status: 201
          - name: rest
            response:
              status: 200
`
	d, err := engine.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	eng := engine.New(d)

	const n = 200
	statuses := make([]float64, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			resp := eng.Resolve(&engine.Request{Protocol: "http", Method: "GET", Path: "/ping"})
			statuses[i], _ = resp["status"].(float64)
		}(i)
	}
	wg.Wait()

	created := 0
	for _, s := range statuses {
		if s == 201 {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one request to observe calls==1 (status 201), got %d", created)
	}
}
