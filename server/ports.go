package server

import (
	"fmt"

	"github.com/openmock-dev/openmock-go/engine"
)

// The stable default ports of the serving profile (docs/serving.md §2) and
// the admin API port (§3). An automatically assigned port must never be one
// of these (§2); binding port 0 satisfies that by construction.
const (
	defaultHTTPPort      = 3000
	defaultGraphQLPort   = 3001
	defaultWebSocketPort = 3002
	defaultGRPCPort      = 3003
	defaultAdminPort     = 4400
)

// reservedPorts are the profile's stable defaults; an auto-assigned port
// (ephemeral or one the implementation picks) must avoid them (§2).
var reservedPorts = map[int]bool{
	defaultHTTPPort:      true,
	defaultGraphQLPort:   true,
	defaultWebSocketPort: true,
	defaultGRPCPort:      true,
	defaultAdminPort:     true,
}

// portSource records which rung of the resolution order (§2) supplied a
// port, so a bind failure can be reported against its origin.
type portSource int

const (
	sourceConfig    portSource = iota // explicit --port / OPENMOCK_PORTS
	sourceHint                        // the document's port hint
	sourceDefault                     // the sole-server stable default
	sourceEphemeral                   // OS-assigned (bind 0)
)

func (s portSource) String() string {
	switch s {
	case sourceConfig:
		return "explicit configuration"
	case sourceHint:
		return "the document's port hint"
	case sourceDefault:
		return "the sole-server default"
	default:
		return "an ephemeral assignment"
	}
}

// resolvedPort is a server's resolved listening port. A port of 0 means an
// OS-assigned ephemeral port, settled when the listener is bound.
type resolvedPort struct {
	name   string
	port   int
	source portSource
}

// typeDefaultPort returns the stable default port for a server type
// (docs/serving.md §2), and whether the type has one this adapter can serve.
// All four v0.2 protocols are served.
func typeDefaultPort(t engine.ServerType) (int, bool) {
	switch t {
	case engine.ServerHTTP:
		return defaultHTTPPort, true
	case engine.ServerGRPC:
		return defaultGRPCPort, true
	case engine.ServerGraphQL:
		return defaultGraphQLPort, true
	case engine.ServerWebSocket:
		return defaultWebSocketPort, true
	default:
		return 0, false
	}
}

// resolvePorts resolves a listening port for every servable server in the
// document (HTTP and gRPC), first-match-wins over the four rungs of
// docs/serving.md §2: explicit config, the document hint, the sole-server
// default (only when the document declares exactly one server of a type),
// then an ephemeral fallback. Servers of not-yet-served types are skipped
// here (surfaced as unbound in the admin topology).
//
// It enforces the profile's static rules: two servers on the same concrete
// port is an error, and a config/hint port that collides with the admin port
// is an error. The "auto-assigned port must avoid a reserved default" rule is
// satisfied structurally because the only auto-assignment is bind-0.
func resolvePorts(doc *engine.Document, explicit map[string]int, adminPort int) ([]resolvedPort, error) {
	countByType := map[engine.ServerType]int{}
	for _, s := range doc.Servers {
		countByType[s.Type]++
	}

	var resolved []resolvedPort
	used := map[int]string{} // concrete port -> server name
	for _, s := range doc.Servers {
		def, servable := typeDefaultPort(s.Type)
		if !servable {
			continue
		}
		rp := resolvedPort{name: s.Name}
		switch {
		case explicit[s.Name] != 0:
			rp.port, rp.source = explicit[s.Name], sourceConfig
		case s.Port != nil:
			rp.port, rp.source = *s.Port, sourceHint
		case countByType[s.Type] == 1:
			rp.port, rp.source = def, sourceDefault
		default:
			rp.port, rp.source = 0, sourceEphemeral
		}

		if rp.port != 0 {
			if other, clash := used[rp.port]; clash {
				return nil, fmt.Errorf("port %d resolved for both %q and %q", rp.port, other, s.Name)
			}
			if rp.port == adminPort {
				return nil, fmt.Errorf("server %q resolved to port %d, which is the admin port", s.Name, rp.port)
			}
			used[rp.port] = s.Name
		}
		resolved = append(resolved, rp)
	}
	return resolved, nil
}
