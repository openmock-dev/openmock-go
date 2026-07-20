// Package server adapts the engine to network protocols and serves it.
//
// [Handler] is the HTTP protocol adapter (milestone 2): a thin http.Handler
// that maps a real HTTP request onto the engine's normalized request and the
// engine's response back onto the wire. gRPC is served too, over the same
// engine, using a document's descriptorSet (spec §3.4) to translate protobuf
// wire bytes to and from the engine's proto3-JSON message model with dynamic
// messages — no generated stubs. GraphQL is served over HTTP
// (GraphQL-over-HTTP): the adapter derives the operation type and name from
// the query document and returns the engine's data/errors/extensions. When a
// GraphQL server carries a schema (spec §3.5), it is loaded to answer
// introspection queries (__schema/__type) so tooling can explore the mock;
// this is additive and never changes how a normal request resolves. WebSocket
// is served over an HTTP upgrade: the connection is routed once at
// establishment (an unroutable path is refused, not accepted, spec §5.9), then
// every client message resolves against the operation, keyed by a
// per-connection call counter.
//
// [Serve] runs the standalone-server serving profile of openmock-dev/openmock's
// docs/serving.md — port resolution (§2), one listener per server in a document
// (all four v0.2 protocols), and the admin/discovery API (§3: GET /servers,
// GET /health, POST /reset).
//
// A gRPC server that carries no descriptorSet (whose messages cannot be
// decoded without one) is reported as unbound in the admin topology.
package server
