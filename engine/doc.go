// Package engine implements OpenMock's pure parse -> match -> render core.
//
// The engine has no I/O and makes no assumption that it runs inside a
// server: it does not bind ports, does not know about HTTP, and depends on
// nothing beyond an OpenMock document and a normalized request. That is what
// lets it be imported standalone by an embedding host (see the "embedding
// host" role in openmock-dev/openmock's docs/serving.md) as well as driven
// by this module's own server and cmd/openmock packages.
//
// Not yet implemented. This package is currently a scaffold placeholder; the
// parse, match, and render stages will be built out and validated against
// the conformance corpus in openmock-dev/openmock's /conformance directory.
package engine
