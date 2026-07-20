# openmock-go

Go reference implementation of [OpenMock](https://github.com/openmock-dev/openmock).

**Status: scaffold stage.** This repository currently contains only the
module layout and package boundaries described below — there is no
parse/match/render logic yet. Nothing here should be depended on for actual
mocking today.

OpenMock is first a **specification**; this repository is its Go reference
tooling. See [openmock-dev/openmock](https://github.com/openmock-dev/openmock)
for:

- the [specification](https://github.com/openmock-dev/openmock/tree/main/spec)
- the [conformance corpus](https://github.com/openmock-dev/openmock/tree/main/conformance)
  that this engine will be validated against once implemented
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
server/       protocol adapters (HTTP today; gRPC, GraphQL, WebSocket to
              follow) over the engine, plus the admin/discovery API from
              docs/serving.md, for the standalone-server role.
cmd/openmock/ the CLI: validate, serve, test subcommands (not yet built).
```

## Status

Every package is currently a placeholder. See each package's doc comment
(`engine/doc.go`, `server/doc.go`) and `cmd/openmock/main.go` for what's
planned and what's not yet there.

## License

MIT, see [LICENSE](./LICENSE).
