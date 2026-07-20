// Package engine implements OpenMock's pure parse -> match -> render core.
//
// The engine has no I/O and makes no assumption that it runs inside a
// server: it does not bind ports, does not know about HTTP, and depends on
// nothing beyond an OpenMock document and a normalized request. That is what
// lets it be imported standalone by an embedding host (see the "embedding
// host" role in openmock-dev/openmock's docs/serving.md) as well as driven
// by this module's own server and cmd/openmock packages.
//
// Use Parse to load and validate a document (spec §3, §3.6) and New to
// create an Engine that resolves normalized Requests against it (spec §8).
// Parse refuses any document spec §3.6 requires an implementation to
// refuse; Resolve maintains the per-operation call counters the calls facet
// reads (spec §6.3) and renders synthetic unrouted/unmatched responses per
// spec §10.
//
// This package passes every case in openmock-dev/openmock's conformance
// corpus (vendored under engine/testdata/conformance; see
// conformance_test.go) covering HTTP, gRPC, GraphQL, and WebSocket mocks:
// routing, first-match-wins scenario selection, all v0.2 matchers
// (equality, exists, RE2 pattern), the calls facet, templating (including
// whole-value typed injection and literal escaping), conformance-mode
// faker, and document validation.
package engine
