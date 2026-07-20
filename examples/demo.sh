#!/usr/bin/env bash
#
# A self-contained walkthrough of `openmock serve`: it starts the server on
# an example, discovers the ports it bound from the startup log, runs a
# scripted sequence of curls, then shuts the server down. Run it with
# `make demo` or `./examples/demo.sh`.
#
# It uses an ephemeral admin port and lets the document's sole HTTP server
# take its port, so it does not fight anything already on 3000/4400 — the
# actual URLs are read back from the server's own log.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(dirname "$here")"
bin="$root/bin/openmock"
doc="$root/examples/05-polling.yml"

command -v curl >/dev/null || { echo "demo needs curl on PATH" >&2; exit 1; }
[ -x "$bin" ] || go build -o "$bin" "$root/cmd/openmock"

log="$(mktemp)"
# Sole HTTP server -> its stable default 3000; admin on an ephemeral port so
# the demo never collides with an existing admin server.
"$bin" serve "$doc" --admin-port 0 >"$log" 2>&1 &
server_pid=$!
cleanup() {
	kill "$server_pid" 2>/dev/null || true
	wait "$server_pid" 2>/dev/null || true
	rm -f "$log"
}
trap cleanup EXIT

# Wait for the two "on http://..." lines the server logs once bound.
mock_url="" admin_url=""
for _ in $(seq 1 50); do
	mock_url=$(sed -n 's/.*serving "main" on \(http[^ ]*\).*/\1/p' "$log" | head -1)
	admin_url=$(sed -n 's/.*admin API on \(http[^ ]*\).*/\1/p' "$log" | head -1)
	[ -n "$mock_url" ] && [ -n "$admin_url" ] && break
	# If the server died (e.g. port in use), surface its log and stop.
	kill -0 "$server_pid" 2>/dev/null || { echo "server exited early:" >&2; cat "$log" >&2; exit 1; }
	sleep 0.1
done
[ -n "$mock_url" ] && [ -n "$admin_url" ] || { echo "server did not report its ports:" >&2; cat "$log" >&2; exit 1; }

hr() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }
show() { printf '\033[36m$ %s\033[0m\n' "$*"; "$@"; echo; }

echo "mock server: $mock_url"
echo "admin API:   $admin_url"

hr "admin: GET /servers (resolved topology)"
show curl -s "$admin_url/servers"

hr "admin: GET /health"
show curl -s "$admin_url/health"

hr "polling lifecycle for one file hash (calls facet)"
echo "The GET advances a per-path counter: 404 (1st) -> pending (2nd-3rd) -> done."
show curl -s -o /dev/null -w 'GET /v1/file/abc -> %{http_code}\n' "$mock_url/v1/file/abc"
show curl -s -w '\n' "$mock_url/v1/file/abc"
show curl -s -w '\n' "$mock_url/v1/file/abc"
show curl -s -w '\n' "$mock_url/v1/file/abc"

hr "admin: POST /reset (clear counters), then poll again"
show curl -s -X POST "$admin_url/reset"
echo "counter reset -> the 1st-call scenario fires again:"
show curl -s -o /dev/null -w 'GET /v1/file/abc -> %{http_code}\n' "$mock_url/v1/file/abc"

hr "an unknown route is an OpenMock synthetic 404 (unrouted)"
show curl -s -w '\n' "$mock_url/nope"

hr "done"
echo "The server shuts down when this script exits."
