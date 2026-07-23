# openmock-go

[![CI](https://github.com/openmock-dev/openmock-go/actions/workflows/ci.yml/badge.svg)](https://github.com/openmock-dev/openmock-go/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/openmock-dev/openmock-go)](https://github.com/openmock-dev/openmock-go/releases)

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
- **gRPC** servers need a `descriptorSet` (spec §3.4) — the compiled
  `FileDescriptorSet` the transport uses to decode/encode protobuf, which is
  not self-describing. The field is optional in the *document* (the document
  stays valid, and the engine/embedding hosts never need it), but it is
  **effectively required for standalone gRPC serving**: a gRPC server without
  one is not bound — it's listed in `GET /servers` with a `note` saying why,
  and the startup log shows how to fix it. Compile one with
  `protoc --include_imports --descriptor_set_out=schema.binpb your.proto`
  (or `buf build -o schema.binpb`), then point a client at the mock using
  that same file as a protoset (see [`examples/`](./examples)).
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
$ make unit       # engine + cmd tests, skipping the conformance corpus
$ make conformance # the vendored openmock-dev/openmock conformance corpus
$ make e2e        # server package + CLI walkthrough, end to end
$ make run EXAMPLE=examples/05-polling.yml   # serve an example in the foreground
```

## CI and releases

[`.github/workflows/ci.yml`](./.github/workflows/ci.yml) runs on every push
and pull request:

- **lint** — `gofmt`, `go vet`, [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)
- **unit** — `engine` and `cmd` tests, minus the conformance corpus
- **conformance** — the vendored corpus from
  [openmock-dev/openmock](https://github.com/openmock-dev/openmock)
- **e2e** — `server` package tests (real HTTP/gRPC/GraphQL/WebSocket
  listeners) plus the scripted CLI walkthrough (`examples/demo.sh`), on
  Linux, macOS, and Windows
- **build** — cross-compiles `cmd/openmock` for every release target
  (linux/darwin/windows × amd64/arm64) so a platform-specific build break
  surfaces before a tag is ever cut

**Tags are cut automatically.** Every push to `main`
([`.github/workflows/auto-tag.yml`](./.github/workflows/auto-tag.yml)) bumps
a semver tag: a `feat:` commit bumps minor, a `BREAKING CHANGE`/`!` commit
bumps major, anything else bumps patch — so a merge is a release unless its
commit message contains `[skip release]`. Pushing that tag then runs:

- [`.github/workflows/release.yml`](./.github/workflows/release.yml) — the
  test suite once more as a safety gate, then
  [GoReleaser](https://goreleaser.com) (config:
  [`.goreleaser.yaml`](./.goreleaser.yaml)), which builds `openmock` for
  Linux, macOS, and Windows (amd64/arm64), publishes `.tar.gz`/`.zip`
  archives and checksums to a GitHub Release, and generates release notes
  from the merged pull requests since the last tag. Try it locally without
  publishing: `make release-snapshot` (needs
  [`goreleaser`](https://goreleaser.com/install/) installed).
- [`.github/workflows/docker.yml`](./.github/workflows/docker.yml) — builds
  and pushes the container image (see [Docker](#docker) below) for
  `linux/amd64` and `linux/arm64` to GHCR, tagged with the full version, its
  `major.minor` and `major`, and `latest`. The same workflow also runs on
  every push to `main`, publishing an `edge` tag.

## Docker

Images are published to
[`ghcr.io/openmock-dev/openmock-go`](https://github.com/openmock-dev/openmock-go/pkgs/container/openmock-go):

```console
$ docker run --rm -p 3000:3000 -p 4400:4400 \
    -v "$(pwd)/examples/01-minimal.yml:/mocks/mocks.yml:ro" \
    ghcr.io/openmock-dev/openmock-go:latest \
    serve mocks.yml --bind 0.0.0.0
$ curl -s localhost:3000/health
```

`--bind 0.0.0.0` is required — the server's default `127.0.0.1` only accepts
connections from inside the container. Map whichever of `3000`–`3003`
(http/graphql/websocket/grpc) and `4400` (admin) your document's servers
resolve to (see [Serving](#serving) above); `docker run ... ghcr.io/openmock-dev/openmock-go:latest version`
prints the build's version/commit/date.

Image tags: `latest` and `vX.Y.Z`/`X.Y`/`X` track releases; `edge` tracks the
tip of `main`.

[`Dockerfile`](./Dockerfile) is a two-stage, fully distroless build, both
stages from [Chainguard](https://github.com/chainguard-images) for a minimal,
continuously patched, low-CVE footprint end to end: the build stage
(`cgr.dev/chainguard/go`) compiles a static (`CGO_ENABLED=0`) binary, and the
runtime stage (`cgr.dev/chainguard/static`) is just that binary,
ca-certificates, and a nonroot user — no shell, no package manager, nothing
else. The resulting image is a few MB. There's no shell to `docker exec`
into; debug from `docker logs` or by running the binary directly. Build it
yourself with `make docker-build` / run with `make docker-run`.

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
