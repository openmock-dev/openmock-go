// Package server adapts the engine to network protocols and serves it.
//
// [Handler] is the HTTP protocol adapter (milestone 2): a thin http.Handler
// that maps a real HTTP request onto the engine's normalized request and the
// engine's response back onto the wire. [Serve] runs the standalone-server
// serving profile of openmock-dev/openmock's docs/serving.md — port
// resolution (§2), an http.Server per HTTP server in a document, and the
// admin/discovery API (§3: GET /servers, GET /health, POST /reset).
//
// Only HTTP is served so far; gRPC, GraphQL, and WebSocket adapters follow
// per the roadmap, and a document's non-HTTP servers are reported as unbound
// in the admin topology until then.
package server
