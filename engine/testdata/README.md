# Vendored conformance corpus

`conformance/` in this directory is a copy of
[`openmock-dev/openmock`](https://github.com/openmock-dev/openmock)'s
[`/conformance`](https://github.com/openmock-dev/openmock/tree/main/conformance)
directory at commit
[`4e79799`](https://github.com/openmock-dev/openmock/commit/4e797996272dbbbdf86c9dfaa45a1d1338dddc09),
minus its `README.md` (case-format documentation, not test data — see the
spec repo for it). It is the normative source of truth for engine behavior
(spec §12); `engine/conformance_test.go` runs every case in it.

To pick up spec/corpus changes, re-copy the spec repo's `conformance/`
directory over this one (dropping its `README.md`) and re-run
`go test ./engine/...`, updating the commit reference above.
