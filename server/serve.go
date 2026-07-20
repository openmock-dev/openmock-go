package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

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
		if srv.Type != engine.ServerHTTP {
			s.topology = append(s.topology, unboundInfo(srv))
			opts.logf("server %q (%s) declared but not served: only HTTP is supported so far", srv.Name, srv.Type)
			continue
		}
		rp := byName[srv.Name]
		addr := fmt.Sprintf("%s:%d", s.bindHost, rp.port)
		l, err := net.Listen("tcp", addr)
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

// Shutdown gracefully stops the mock servers and the admin API.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error
	for _, hs := range s.mockSrvs {
		if err := hs.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.adminSrv != nil {
		if err := s.adminSrv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Server) httpInfo(name string, port int) ServerInfo {
	transport := "http"
	url := fmt.Sprintf("http://%s:%d", dialHost(s.bindHost), port)
	return ServerInfo{Name: name, Type: "http", Transport: &transport, Port: &port, URL: &url}
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
