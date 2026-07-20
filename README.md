# openmock-go

Go reference implementation of [OpenMock](https://github.com/openmock-dev/openmock).

**Status: engine + all four protocol servers implemented.** The `engine`
package is a working parse → match → render core that passes every case in
[openmock-dev/openmock](https://github.com/openmock-dev/openmock)'s
conformance corpus, across all four v0.2 protocols (HTTP, gRPC, GraphQL,
WebSocket). The `server` package serves a document over **HTTP, gRPC, GraphQL,
and WebSocket** — the protocol adapters plus the standalone-server profile of
`docs/serving.md` (port resolution and the admin API) — and `openmock serve`
runs it from the command line. gRPC serving uses a server's `descriptorSet`
(spec §3.4) to translate protobuf wire bytes; GraphQL and WebSocket are served
over HTTP. A gRPC server with no `descriptorSet` (whose messages can't be
decoded) is reported as unbound in the admin topology.

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
server/       protocol adapters + serving (HTTP, gRPC, GraphQL, and
              WebSocket) over the engine, plus the admin/discovery API from
              docs/serving.md, for the standalone-server role.
cmd/openmock/ the CLI: `serve` implemented; validate, test to follow.
```

## Serving

```console
$ openmock serve mocks.yml
serving "main" on http://127.0.0.1:3000
admin API on http://127.0.0.1:4400
```

- **Ports** resolve per `docs/serving.md` §2: `--port name=n` (repeatable) or
  `OPENMOCK_PORTS="a=3000,b=3001"`, then the document's `port` hint, then the
  stable default for a document's sole server of a type (`http` 3000,
  `graphql` 3001, `websocket` 3002, `grpc` 3003), else an OS-ephemeral port. A
  configured/hinted port that can't bind fails loudly.
- **Admin API** on `4400` (`--admin-port`, `0` for ephemeral), loopback by
  default: `GET /servers` (the resolved topology), `GET /health`,
  `POST /reset` (clear call counters).
- **`--bind`** sets the interface (default `127.0.0.1`; `0.0.0.0` to expose).
- **gRPC** servers are served when they carry a `descriptorSet` (spec §3.4) —
  the compiled `FileDescriptorSet` the transport uses to decode/encode
  protobuf. A gRPC server without one is listed unbound. Point a client at it
  with that same file as a protoset (see [`examples/`](./examples)).
- **GraphQL** servers are served over HTTP (GraphQL-over-HTTP): POST (or GET) a
  `{query, operationName, variables}` request to the port; the operation type
  and name are read from the query document. No schema is needed to serve —
  but if a server declares a `schema` (spec §3.5), it powers **introspection**
  (`__schema`/`__type`), so GraphiQL/Apollo/Postman can explore the mock. A
  missing or invalid schema is a warning, not an error.
- **WebSocket** servers are served over an HTTP upgrade. The connection is
  routed once by its path at establishment (an unroutable path is refused with
  a 404, never accepted); then every client message is resolved, with a
  per-connection call counter, into zero or more reply messages and an optional
  close.

### Try it

```console
$ make build
$ ./bin/openmock serve examples/01-minimal.yml &
$ curl -s localhost:3000/health          # {"status":"ok"}
$ curl -s localhost:4400/servers         # the resolved topology
```

Or run the scripted, self-contained walkthrough (starts the server, curls a
polling lifecycle and the admin API, shuts down):

```console
$ make demo
```

The [`examples/`](./examples) directory has seven runnable documents —
path params, templating and faker, delays and errors, the `calls` polling
lifecycle, `exists`/`pattern` matchers with multi-line `Set-Cookie`, and a
two-server topology — each with copy-paste curl commands in
[`examples/README.md`](./examples/README.md).

## Development

```console
$ make            # list targets
$ make build      # ./bin/openmock
$ make check      # gofmt check + go vet + go test -race (what CI runs)
$ make run EXAMPLE=examples/05-polling.yml   # serve an example in the foreground
```

## Using the engine directly

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
— e.g. `{"status": 200.0, "headers": {...}, "body": {...}}` for HTTP. The
`server` package maps that onto real HTTP traffic; embedding hosts can drive
`Resolve` directly.

## License

MIT, see [LICENSE](./LICENSE).
