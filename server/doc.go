// Package server adapts the engine to network protocols.
//
// It provides thin protocol adapters (HTTP first, with gRPC, GraphQL, and
// WebSocket to follow per openmock-dev/openmock's roadmap) that map real
// requests onto the engine and the engine's responses back onto the wire.
// It also provides the admin/discovery API described in
// openmock-dev/openmock's docs/serving.md (GET /servers, GET /health,
// POST /reset) for the standalone-server role.
//
// Not yet implemented. This package is currently a scaffold placeholder.
package server
