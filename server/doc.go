// Package server adapts the engine to network protocols and serves it.
//
// [Handler] is the HTTP protocol adapter (milestone 2): a thin http.Handler
// that maps a real HTTP request onto the engine's normalized request and the
// engine's response back onto the wire. gRPC is served too, over the same
// engine, using a document's descriptorSet (spec §3.4) to translate protobuf
// wire bytes to and from the engine's proto3-JSON message model with dynamic
// messages — no generated stubs.
//
// [Serve] runs the standalone-server serving profile of openmock-dev/openmock's
// docs/serving.md — port resolution (§2), one listener per HTTP or gRPC server
// in a document, and the admin/discovery API (§3: GET /servers, GET /health,
// POST /reset).
//
// GraphQL and WebSocket adapters follow per the roadmap; until then, and for a
// gRPC server that carries no descriptorSet (whose messages cannot be decoded),
// such servers are reported as unbound in the admin topology.
package server
