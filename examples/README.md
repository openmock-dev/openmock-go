# Examples

Runnable OpenMock documents for the HTTP server. Build the binary and serve
one, then curl it:

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
`06`–`07` add features they don't cover.

---

## 01-minimal.yml — a single endpoint

```console
$ curl -s localhost:3000/health
{"status":"ok"}
```

## 02-rest.yml — scenarios, `when`, path params, templating

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

## 03-templating-faker.yml — every placeholder namespace + faker

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

## 04-delays-errors.yml — delays and declared errors

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

## 05-polling.yml — the `calls` facet (stateful polling)

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

## 06-cookies-and-matchers.yml — `exists`/`pattern` matchers, multi-line Set-Cookie

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

## 07-multi-server.yml — two HTTP servers in one document

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
