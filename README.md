# openmock-go

Go reference implementation of [OpenMock](https://github.com/openmock-dev/openmock).

**Status: engine implemented, server and CLI not yet started.** The `engine`
package is a working parse → match → render core that passes every case in
[openmock-dev/openmock](https://github.com/openmock-dev/openmock)'s
conformance corpus, across all four v0.2 protocols (HTTP, gRPC, GraphQL,
WebSocket). It has no server and no CLI yet — `server/` and `cmd/openmock/`
are still placeholders, so there is no way to actually serve mocks with this
repository today.

OpenMock is first a **specification**; this repository is its Go reference
tooling. See [openmock-dev/openmock](https://github.com/openmock-dev/openmock)
for:

- the [specification](https://github.com/openmock-dev/openmock/tree/main/spec)
- the [conformance corpus](https://github.com/openmock-dev/openmock/tree/main/conformance)
  this engine is validated against — vendored under
  `engine/testdata/conformance` and run by `go test ./engine/...`
- [`IMPLEMENTATIONS.md`](https://github.com/openmock-dev/openmock/blob/main/IMPLEMENTATIONS.md),
  whose reference-tooling roadmap names this repository's engine as
  **milestone 1** ("Go engine (canonical)"), ahead of the HTTP adapter, the
  CLI, and a future TypeScript port
- [`docs/serving.md`](https://github.com/openmock-dev/openmock/blob/main/docs/serving.md),
  which defines the engine / embedding-host / standalone-server roles that
  this repository's package split follows

## Layout

One Go module, split into three packages along the lines `docs/serving.md`
draws between the resolution engine and the roles that serve it:

```
engine/       pure parse -> match -> render core; no I/O, no server
              assumption. Importable standalone by an embedding host.
              Implemented: HTTP, gRPC, GraphQL, and WebSocket, conformant.
server/       protocol adapters (HTTP today; gRPC, GraphQL, WebSocket to
              follow) over the engine, plus the admin/discovery API from
              docs/serving.md, for the standalone-server role. Placeholder.
cmd/openmock/ the CLI: validate, serve, test subcommands. Placeholder.
```

## Status

- **`engine/`** — implemented. `engine.Parse` loads and validates a document
  (spec §3, refusing anything §3.6 requires refusing); `engine.New(doc)`
  returns an `*Engine` whose `Resolve(*Request)` runs the matching algorithm
  of spec §8 — routing, the call counter, first-match-wins scenario
  selection, and templated rendering (including faker, in the corpus's
  deterministic conformance mode) — for HTTP, gRPC, GraphQL, and WebSocket
  alike. `go test ./engine/...` runs it against every case in
  `engine/testdata/conformance`.
- **`server/`**, **`cmd/openmock/`** — still placeholders. See each
  package's doc comment (`server/doc.go`) and `cmd/openmock/main.go` for
  what's planned.

## Using the engine

```go
doc, err := engine.Parse(yamlBytes) // spec §3; refuses invalid documents
if err != nil {
    log.Fatal(err)
}
eng := engine.New(doc)
resp := eng.Resolve(&engine.Request{
    Protocol: "http",
    Method:   "GET",
    Path:     "/users/42",
})
```

`Resolve` returns a `map[string]any` shaped like the protocol's normalized
response in openmock-dev/openmock's
[`conformance/README.md`](https://github.com/openmock-dev/openmock/blob/main/conformance/README.md#expectedjson-shape)
— e.g. `{"status": 200.0, "headers": {...}, "body": {...}}` for HTTP. A
future `server` package will map that onto real HTTP/gRPC/GraphQL/WebSocket
traffic; today, calling `Resolve` directly is the only way to drive the
engine.

## License

MIT, see [LICENSE](./LICENSE).
