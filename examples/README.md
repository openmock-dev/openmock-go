# Examples

Runnable OpenMock documents. `01`–`07` are HTTP; `08` is gRPC; `09` is
GraphQL; `10` is WebSocket. Build the binary and serve one, then curl it:

```console
$ make build                                   # produces ./bin/openmock
$ ./bin/openmock serve examples/01-minimal.yml
serving "main" on http://127.0.0.1:3000
admin API on http://127.0.0.1:4400
```

A document's sole HTTP server takes the stable default port **3000**; the
admin API is on **4400**. Curl from another terminal. For a hands-off tour of
the whole thing, run `make demo` (or `./examples/demo.sh`), which starts the
server, runs the calls below, and shuts it down.

Every server exposes the same **admin API** on `:4400`:

```console
$ curl -s localhost:4400/servers      # resolved name -> port topology
$ curl -s localhost:4400/health       # {"status":"ok"}
$ curl -s -X POST localhost:4400/reset  # clear call counters
```

`01`–`05` are the reference HTTP examples from the
[spec repo](https://github.com/openmock-dev/openmock/tree/main/examples);
`06`–`07` add HTTP features they don't cover; `08` is gRPC, `09` GraphQL, `10`
WebSocket.

---

## 01-minimal.yml: a single endpoint

```console
$ curl -s localhost:3000/health
{"status":"ok"}
```

## 02-rest.yml: scenarios, `when`, path params, templating

First-match-wins scenarios over `GET /users/{id}` and `POST /users`.

```console
$ curl -s localhost:3000/users/7                       # default scenario
{"id":"7","name":"Anonymous","role":"user"}

$ curl -s -H 'X-Role: admin' localhost:3000/users/7    # admin scenario
{"id":"7","name":"Ada Lovelace","role":"admin"}

$ curl -s localhost:3000/users/1                        # id-specific scenario
{"id":"1","name":"Grace Hopper","role":"user"}

$ curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"name":"Alan"}' localhost:3000/users           # created
{"id":"100","name":"Alan","role":"user"}

$ curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"name":""}' localhost:3000/users               # missing name -> 422
{"error":"name is required"}
```

## 03-templating-faker.yml: every placeholder namespace + faker

Faker runs in the corpus's deterministic conformance mode, so values are
stable (`Ada Lovelace`, etc.).

```console
$ curl -s -H 'X-Request-Id: r-1' 'localhost:3000/users/42?fields=full'
{"id":"42","fields":"full","name":"Ada Lovelace",
 "email":"ada.lovelace@example.com","token":"3f2504e0-4f89-11d3-9a0c-0305e82c3301",
 "createdAt":"2026-01-01T12:00:00.000Z"}
# response also echoes X-Request-Id back as a header:
$ curl -s -D - -o /dev/null -H 'X-Request-Id: r-1' localhost:3000/users/42 | grep -i x-request-id

$ curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"user":{"name":"Ada","role":"admin"}}' localhost:3000/echo
{"receivedName":"Ada","receivedRole":"admin"}
```

## 04-delays-errors.yml: delays and declared errors

```console
$ time curl -s localhost:3000/orders/slow      # declared delay: 1500ms
{"id":"slow","status":"processing"}

$ curl -s -o /dev/null -w '%{http_code}\n' localhost:3000/orders/boom   # 503
$ curl -s localhost:3000/orders/xyz            # declared 404 (not the synthetic)
{"error":"order not found","id":"xyz"}
```

Note the difference from OpenMock's own synthetics: an unknown *route* returns
the `{"openmock":"unrouted",...}` 404; the `order not found` above is an
author-declared response.

## 05-polling.yml: the `calls` facet (stateful polling)

The GET counter is per concrete path, so `abc` and `def` advance
independently. Reset clears it.

```console
$ curl -s -o /dev/null -w '%{http_code}\n' localhost:3000/v1/file/abc   # 404 (1st)
$ curl -s localhost:3000/v1/file/abc     # {"hash":"abc","status":"pending"}  (2nd-3rd)
$ curl -s localhost:3000/v1/file/abc     # pending
$ curl -s localhost:3000/v1/file/abc     # {"hash":"abc","status":"done","verdict":"clean"}
$ curl -s -X POST localhost:4400/reset   # counters cleared
$ curl -s -o /dev/null -w '%{http_code}\n' localhost:3000/v1/file/abc   # 404 again
```

## 06-cookies-and-matchers.yml: `exists`/`pattern` matchers, multi-line Set-Cookie

```console
$ curl -s -D - -H 'X-Api-Key: key-123' -X POST localhost:3000/login
# 200, with TWO Set-Cookie lines (session=...; and theme=dark):
#   Set-Cookie: session=3f2504e0-...; Path=/; HttpOnly
#   Set-Cookie: theme=dark; Path=/

$ curl -s -X POST localhost:3000/login                          # no key -> 401
{"error":"api key required"}

$ curl -s -H 'X-Api-Key: nope' -X POST localhost:3000/login     # bad key -> 403
{"error":"invalid api key"}
```

## 07-multi-server.yml: two HTTP servers in one document

Two servers, addressed by the port they're served on. With two servers of a
type the sole-server default doesn't apply, so give each a port (the document
hints `3000`/`3001`; override with flags or the environment):

```console
$ ./bin/openmock serve examples/07-multi-server.yml \
    --port users=3000 --port orders=3001
# or:  OPENMOCK_PORTS="users=3000,orders=3001" ./bin/openmock serve examples/07-multi-server.yml

$ curl -s localhost:3000/health      # {"service":"users","status":"ok"}
$ curl -s localhost:3001/health      # {"service":"orders","status":"ok"}
$ curl -s localhost:4400/servers     # both servers, with their resolved ports
```

## 08-grpc.yml: a gRPC mock

A `type: grpc` server. It attaches a compiled descriptor set
(`08-grpc.binpb`, built from `08-grpc.proto`), which the server uses to
decode and encode protobuf on the wire. A document's sole gRPC server takes
the stable default port **3003**.

The `descriptorSet` is optional in the *document* (spec §3.4 — the mock stays
valid and the engine never needs it), but **standalone gRPC serving requires
it**: protobuf is not self-describing, so without the schema the wire can't be
decoded. A gRPC server without one is left unbound, with the reason in the
startup log and in `GET /servers` as a `note`. Regenerate the descriptor from
the `.proto` with either:

```console
$ protoc --include_imports --descriptor_set_out=08-grpc.binpb 08-grpc.proto
$ buf build 08-grpc.proto -o 08-grpc.binpb     # equivalent, with buf
```

```console
$ ./bin/openmock serve examples/08-grpc.yml
serving "main" on grpc://127.0.0.1:3003
admin API on http://127.0.0.1:4400
```

Call it with [`grpcurl`](https://github.com/fullstorydev/grpcurl), using the
same `.binpb` as its protoset (so no `.proto` or server reflection is needed):

```console
$ grpcurl -protoset examples/08-grpc.binpb -plaintext \
    -d '{"orderId":"42"}' localhost:3003 shop.v1.OrderService/GetOrder
{"id": "42", "state": "SHIPPED"}

# metadata selects the admin scenario, which returns an x-cache header:
$ grpcurl -protoset examples/08-grpc.binpb -plaintext \
    -H 'x-role: admin' -d '{"orderId":"7"}' \
    localhost:3003 shop.v1.OrderService/GetOrder

# orderId 0 -> a NOT_FOUND status with a templated message:
$ grpcurl -protoset examples/08-grpc.binpb -plaintext \
    -d '{"orderId":"0"}' localhost:3003 shop.v1.OrderService/GetOrder
ERROR: Code: NotFound  Message: order 0 not found

# server-streaming: three ordered events:
$ grpcurl -protoset examples/08-grpc.binpb -plaintext \
    -d '{"orderId":"42"}' localhost:3003 shop.v1.OrderService/WatchOrder
```

The admin API's `GET /servers` lists the gRPC server with `"transport":
"grpc"` and a null `url` (gRPC has no dialable base URL). `grpcurl` is
optional. The wire behavior is covered by `server/grpc_test.go`, which
drives the server with a dynamic gRPC client.

## 09-graphql.yml: a GraphQL mock

A `type: graphql` server, served over HTTP (GraphQL-over-HTTP). A document's
sole GraphQL server takes the stable default port **3001**. POST a
`{query, operationName, variables}` body to any path on the port; the
operation type and name are read from the query. The optional `schema` field
is not needed to serve.

```console
$ ./bin/openmock serve examples/09-graphql.yml
serving "main" on graphql http://127.0.0.1:3001
admin API on http://127.0.0.1:4400

# a query with variables templating:
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -d \
    '{"query":"query GetOrder { order { id state } }","variables":{"orderId":"42"}}'
{"data":{"order":{"id":"42","state":"SHIPPED"}}}

# a header facet selects the admin scenario (richer payload):
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -H 'x-role: admin' -d \
    '{"query":"query GetOrder { order { id } }","variables":{"orderId":"7"}}'

# a mutation (routing is on operationType + operationName):
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -d \
    '{"query":"mutation CreateOrder { createOrder { id } }","variables":{"input":{"note":"rush"}}}'

# a partial response — data plus errors — for a missing order:
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -d \
    '{"query":"query GetOrder { order { id } }","variables":{"orderId":"0"}}'
{"data":{"order":null},"errors":[{"message":"order not found","path":["order"],...}]}
```

Failures are in-band: a matched-but-erroring or unrouted request still returns
HTTP `200` with an `errors` array (spec §7.4/§10). A malformed request (no
`query`, bad JSON) returns `400`.

This example declares a `schema` (`09-graphql.graphql`), so the mock answers
**introspection**: point GraphiQL/Apollo/Postman at `http://localhost:3001/`
to explore it, or curl it directly:

```console
# the schema's types, queryType, mutationType (the IntrospectionQuery tools send):
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -d \
    '{"query":"{ __schema { queryType { name } types { name kind } } }"}'

# a single type by name:
$ curl -s localhost:3001/ -H 'Content-Type: application/json' -d \
    '{"query":"{ __type(name: \"Order\") { name kind fields { name } } }"}'
```

Introspection is additive. It never changes how a normal request resolves
(spec §3.5), and the `schema` field is optional: without it, everything above
still works except introspection.

## 10-websocket.yml: a WebSocket mock

A `type: websocket` server, served over an HTTP upgrade. A document's sole
WebSocket server takes the stable default port **3002**. A client connects to
a path (`/ws/orders/{id}`); each message it sends is resolved into zero or more
reply messages and an optional close. The call counter is per connection, so
every connection's scripted exchange starts fresh.

```console
$ ./bin/openmock serve examples/10-websocket.yml
serving "main" on ws://127.0.0.1:3002
admin API on http://127.0.0.1:4400
```

Drive it with any WebSocket client (`websocat` shown; the connection path
carries the `{id}` param):

```console
$ websocat ws://127.0.0.1:3002/ws/orders/42
{"type":"status"}
{"orderId":"42","type":"welcome"}      # first message on a connection -> welcome (calls:1)
{"type":"status"}
{"orderId":"42","state":"SHIPPED","type":"status"}
{"type":"ping"}                        # a heartbeat is ignored (empty reply), connection stays open
{"type":"bye"}
{"type":"goodbye"}                     # reply, then the server closes with code 1000
```

An unroutable path is **refused at establishment**: the upgrade returns a
`404` with the `unrouted` synthetic rather than accepting the connection (spec
§5.9). A matched message with no scenario/default gets a single `unmatched`
reply message while the connection stays open (spec §10.1).
