package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"google.golang.org/grpc"

	"github.com/openmock-dev/openmock-go/engine"
)

// Options configures a standalone HTTP Server (docs/serving.md).
type Options struct {
	// Bind is the interface mock servers and the admin API listen on.
	// Empty defaults to 127.0.0.1 (loopback); "0.0.0.0" exposes beyond it.
	Bind string
	// Ports maps server name to an explicit port (rung 1 of §2), from
	// --port / OPENMOCK_PORTS.
	Ports map[string]int
	// AdminPort is the admin API port; 0 uses the default 4400. Use
	// AdminPortEphemeral to request an OS-assigned admin port.
	AdminPort int
	// AdminPortEphemeral binds the admin API on an OS-assigned port
	// (--admin-port 0), reported via Logf and Server.AdminAddr.
	AdminPortEphemeral bool
	// Logf receives startup diagnostics (the name -> port topology, §2). Nil
	// discards them.
	Logf func(format string, args ...any)
	// BaseDir is the directory a gRPC server's descriptorSet path is resolved
	// against (spec §3.4/§14). Empty means the current directory. The CLI
	// sets it to the document's directory.
	BaseDir string
}

func (o Options) bindHost() string {
	if o.Bind == "" {
		return "127.0.0.1"
	}
	return o.Bind
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// ServerInfo is one entry of the admin GET /servers topology
// (docs/serving.md §3). Port and URL are null (nil) for a server this
// process does not bind.
type ServerInfo struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Transport *string `json:"transport"`
	Port      *int    `json:"port"`
	URL       *string `json:"url"`
}

// Server is a running standalone HTTP server for one document: an http.Server
// per HTTP server in the document, all backed by one engine, plus the admin
// API. Create it with Serve; stop it with Shutdown.
type Server struct {
	engine    *engine.Engine
	bindHost  string
	topology  []ServerInfo
	mockSrvs  []*http.Server
	grpcSrvs  []*grpc.Server
	adminSrv  *http.Server
	adminAddr string

	wg       sync.WaitGroup
	mu       sync.Mutex
	serveErr error
}

// Serve resolves ports (§2), binds an HTTP listener for every HTTP server in
// doc, mounts the admin API (§3), and starts serving in the background. It
// returns once every listener is bound (so the admin GET /health is
// immediately truthful) or with an error if any bind fails — ports resolved
// from configuration or a hint fail loudly rather than being silently
// reassigned (§2). Non-HTTP servers are reported as unbound in the topology.
func Serve(doc *engine.Document, opts Options) (*Server, error) {
	adminPort := opts.AdminPort
	if adminPort == 0 && !opts.AdminPortEphemeral {
		adminPort = defaultAdminPort
	}
	if opts.AdminPortEphemeral {
		adminPort = 0
	}

	resolved, err := resolvePorts(doc, opts.Ports, adminPort)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]resolvedPort, len(resolved))
	for _, rp := range resolved {
		byName[rp.name] = rp
	}

	s := &Server{engine: engine.New(doc), bindHost: opts.bindHost()}

	// Bind every HTTP listener first; if any fails, close what we opened and
	// return — no partial serving.
	var listeners []net.Listener
	closeAll := func() {
		for _, l := range listeners {
			l.Close()
		}
	}
	for _, srv := range doc.Servers {
		switch srv.Type {
		case engine.ServerHTTP:
			rp := byName[srv.Name]
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.bindHost, rp.port))
			if err != nil {
				closeAll()
				return nil, fmt.Errorf("binding %q to port %d (%s): %w", srv.Name, rp.port, rp.source, err)
			}
			listeners = append(listeners, l)
			port := l.Addr().(*net.TCPAddr).Port
			s.topology = append(s.topology, s.httpInfo(srv.Name, port))
			hs := &http.Server{Handler: NewHandler(s.engine, srv.Name)}
			s.mockSrvs = append(s.mockSrvs, hs)
			s.serveOn(hs, l)
			opts.logf("serving %q on http://%s:%d", srv.Name, dialHost(s.bindHost), port)

		case engine.ServerGRPC:
			// A gRPC server is served only with a descriptorSet — its schema
			// is what lets the transport decode and encode wire bytes (spec
			// §3.4). Without a loadable one, leave it unbound with a
			// diagnostic, exactly as if the field were absent.
			if srv.DescriptorSet == "" {
				s.topology = append(s.topology, unboundInfo(srv))
				opts.logf("server %q (grpc) declared but not served: no descriptorSet (needed to decode protobuf)", srv.Name)
				continue
			}
			schema, err := loadDescriptor(opts.BaseDir, srv)
			if err != nil {
				s.topology = append(s.topology, unboundInfo(srv))
				opts.logf("server %q (grpc) declared but not served: %v", srv.Name, err)
				continue
			}
			rp := byName[srv.Name]
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.bindHost, rp.port))
			if err != nil {
				closeAll()
				return nil, fmt.Errorf("binding %q to port %d (%s): %w", srv.Name, rp.port, rp.source, err)
			}
			listeners = append(listeners, l)
			port := l.Addr().(*net.TCPAddr).Port
			s.topology = append(s.topology, s.grpcInfo(srv.Name, port))
			gs := newGRPCServer(s.engine, srv.Name, schema)
			s.grpcSrvs = append(s.grpcSrvs, gs)
			s.serveGRPC(gs, l)
			opts.logf("serving %q on grpc://%s:%d", srv.Name, dialHost(s.bindHost), port)

		case engine.ServerGraphQL:
			// GraphQL is served over HTTP; its messages are already JSON, so
			// no schema is needed to serve (spec §3.5). A schema, when present
			// and loadable, adds introspection; a missing/unreadable one is a
			// diagnostic, not an error (spec §3.5).
			var introspection *graphqlIntrospection
			if srv.Schema != "" {
				gi, err := loadGraphQLSchema(opts.BaseDir, srv)
				if err != nil {
					opts.logf("server %q: schema not loaded (%v); serving without introspection", srv.Name, err)
				} else {
					introspection = gi
				}
			}
			rp := byName[srv.Name]
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.bindHost, rp.port))
			if err != nil {
				closeAll()
				return nil, fmt.Errorf("binding %q to port %d (%s): %w", srv.Name, rp.port, rp.source, err)
			}
			listeners = append(listeners, l)
			port := l.Addr().(*net.TCPAddr).Port
			s.topology = append(s.topology, s.graphqlInfo(srv.Name, port))
			hs := &http.Server{Handler: newGraphQLHandler(s.engine, srv.Name, introspection)}
			s.mockSrvs = append(s.mockSrvs, hs)
			s.serveOn(hs, l)
			schemaNote := ""
			if introspection != nil {
				schemaNote = " (introspection enabled)"
			}
			opts.logf("serving %q on graphql http://%s:%d%s", srv.Name, dialHost(s.bindHost), port, schemaNote)

		case engine.ServerWebSocket:
			// WebSocket is served over an HTTP upgrade; like GraphQL its
			// messages are JSON, so no schema is needed.
			rp := byName[srv.Name]
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.bindHost, rp.port))
			if err != nil {
				closeAll()
				return nil, fmt.Errorf("binding %q to port %d (%s): %w", srv.Name, rp.port, rp.source, err)
			}
			listeners = append(listeners, l)
			port := l.Addr().(*net.TCPAddr).Port
			s.topology = append(s.topology, s.websocketInfo(srv.Name, port))
			hs := &http.Server{Handler: newWSHandler(s.engine, srv.Name)}
			s.mockSrvs = append(s.mockSrvs, hs)
			s.serveOn(hs, l)
			opts.logf("serving %q on ws://%s:%d", srv.Name, dialHost(s.bindHost), port)

		default:
			s.topology = append(s.topology, unboundInfo(srv))
			opts.logf("server %q (%s) declared but not served: that protocol is not supported yet", srv.Name, srv.Type)
		}
	}

	// Admin API.
	adminAddr := fmt.Sprintf("%s:%d", s.bindHost, adminPort)
	al, err := net.Listen("tcp", adminAddr)
	if err != nil {
		closeAll()
		return nil, fmt.Errorf("binding admin API to port %d: %w", adminPort, err)
	}
	s.adminAddr = al.Addr().String()
	s.adminSrv = &http.Server{Handler: s.adminHandler()}
	s.serveOn(s.adminSrv, al)
	opts.logf("admin API on http://%s:%d", dialHost(s.bindHost), al.Addr().(*net.TCPAddr).Port)

	return s, nil
}

func (s *Server) serveOn(hs *http.Server, l net.Listener) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := hs.Serve(l); err != nil && err != http.ErrServerClosed {
			s.mu.Lock()
			if s.serveErr == nil {
				s.serveErr = err
			}
			s.mu.Unlock()
		}
	}()
}

func (s *Server) serveGRPC(gs *grpc.Server, l net.Listener) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// grpc.Server.Serve returns nil on GracefulStop/Stop.
		if err := gs.Serve(l); err != nil && err != grpc.ErrServerStopped {
			s.mu.Lock()
			if s.serveErr == nil {
				s.serveErr = err
			}
			s.mu.Unlock()
		}
	}()
}

// Topology returns the served name -> port mapping (admin GET /servers).
func (s *Server) Topology() []ServerInfo { return s.topology }

// AdminAddr is the admin API's bound address (host:port), useful when the
// admin port was ephemeral.
func (s *Server) AdminAddr() string { return s.adminAddr }

// Wait blocks until every server has stopped (after Shutdown).
func (s *Server) Wait() { s.wg.Wait() }

// Err returns the first non-graceful serving error, if any.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serveErr
}

// Shutdown gracefully stops the mock servers (HTTP and gRPC) and the admin
// API. A gRPC server is drained with GracefulStop, falling back to a hard
// Stop if ctx is cancelled first.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error
	for _, hs := range s.mockSrvs {
		if err := hs.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, gs := range s.grpcSrvs {
		s.stopGRPC(ctx, gs)
	}
	if s.adminSrv != nil {
		if err := s.adminSrv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stopGRPC drains gs gracefully, but stops hard if ctx is cancelled first so
// a client holding a stream open cannot block shutdown past the deadline.
func (s *Server) stopGRPC(ctx context.Context, gs *grpc.Server) {
	done := make(chan struct{})
	go func() {
		gs.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		gs.Stop()
		<-done
	}
}

func (s *Server) httpInfo(name string, port int) ServerInfo {
	transport := "http"
	url := fmt.Sprintf("http://%s:%d", dialHost(s.bindHost), port)
	return ServerInfo{Name: name, Type: "http", Transport: &transport, Port: &port, URL: &url}
}

// grpcInfo reports a bound gRPC server. Per docs/serving.md §3 the topology
// carries its transport and resolved port but a null url — gRPC has no
// dialable base URL like HTTP.
func (s *Server) grpcInfo(name string, port int) ServerInfo {
	transport := "grpc"
	return ServerInfo{Name: name, Type: "grpc", Transport: &transport, Port: &port, URL: nil}
}

// graphqlInfo reports a bound GraphQL server. It is served over HTTP, so it
// has a dialable url; transport names the protocol a client speaks there.
func (s *Server) graphqlInfo(name string, port int) ServerInfo {
	transport := "graphql"
	url := fmt.Sprintf("http://%s:%d", dialHost(s.bindHost), port)
	return ServerInfo{Name: name, Type: "graphql", Transport: &transport, Port: &port, URL: &url}
}

// websocketInfo reports a bound WebSocket server with a dialable ws:// base
// URL; a client appends the connection path.
func (s *Server) websocketInfo(name string, port int) ServerInfo {
	transport := "websocket"
	url := fmt.Sprintf("ws://%s:%d", dialHost(s.bindHost), port)
	return ServerInfo{Name: name, Type: "websocket", Transport: &transport, Port: &port, URL: &url}
}

func unboundInfo(srv *engine.Server) ServerInfo {
	return ServerInfo{Name: srv.Name, Type: string(srv.Type)}
}

// dialHost turns a bind host into one a client can dial: a wildcard bind
// (0.0.0.0 or empty) is reported as loopback in URLs.
func dialHost(bind string) string {
	if bind == "" || bind == "0.0.0.0" || bind == "::" {
		return "127.0.0.1"
	}
	return bind
}
