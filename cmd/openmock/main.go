// Command openmock is the OpenMock reference CLI. It currently offers the
// `serve` subcommand (the HTTP standalone server); `validate` and `test` are
// planned (see openmock-dev/openmock's reference-tooling roadmap).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
	"github.com/openmock-dev/openmock-go/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "openmock: "+err.Error())
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "openmock: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `openmock - OpenMock reference CLI

Usage:
  openmock serve <file> [flags]     Serve a document over HTTP

Flags for 'serve':
  --bind <addr>          Interface to bind (default 127.0.0.1; 0.0.0.0 to expose)
  --port <name>=<n>      Explicit port for a server (repeatable)
  --admin-port <n>       Admin API port (default 4400; 0 for an ephemeral port)

Environment:
  OPENMOCK_PORTS="a=3000,b=3001"    Explicit ports (--port overrides per name)

Planned: validate, test.
`)
}

// serveFlags holds the parsed 'serve' flags. We parse by hand rather than
// with the flag package so that --port can repeat and merge with the
// OPENMOCK_PORTS environment variable.
type serveFlags struct {
	file      string
	bind      string
	ports     map[string]int
	adminPort int
	adminEph  bool
}

func runServe(args []string) error {
	f, err := parseServeArgs(args)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(f.file)
	if err != nil {
		return err
	}
	doc, err := engine.Parse(data)
	if err != nil {
		return err
	}

	srv, err := server.Serve(doc, server.Options{
		Bind:               f.bind,
		Ports:              f.ports,
		AdminPort:          f.adminPort,
		AdminPortEphemeral: f.adminEph,
		Logf:               func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) },
	})
	if err != nil {
		return err
	}

	// Block until interrupted, then shut down gracefully.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Fprintln(os.Stderr, "openmock: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return err
	}
	srv.Wait()
	return srv.Err()
}

func parseServeArgs(args []string) (serveFlags, error) {
	f := serveFlags{ports: map[string]int{}}

	// Environment ports first, so --port flags can override per name.
	if env := os.Getenv("OPENMOCK_PORTS"); env != "" {
		if err := parsePortList(env, f.ports); err != nil {
			return f, fmt.Errorf("OPENMOCK_PORTS: %w", err)
		}
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		name := arg
		inlineValue, hasInline := "", false
		if eq := strings.IndexByte(arg, '='); eq >= 0 && strings.HasPrefix(arg, "-") {
			name, inlineValue, hasInline = arg[:eq], arg[eq+1:], true
		}
		// value returns the flag's value from either "--flag=value" or the
		// following argument, advancing i in the latter case.
		value := func() (string, error) {
			if hasInline {
				return inlineValue, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			i++
			return args[i], nil
		}

		switch {
		case name == "--bind":
			v, err := value()
			if err != nil {
				return f, err
			}
			f.bind = v
		case name == "--port":
			v, err := value()
			if err != nil {
				return f, err
			}
			if err := parsePortList(v, f.ports); err != nil {
				return f, err
			}
		case name == "--admin-port":
			v, err := value()
			if err != nil {
				return f, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return f, fmt.Errorf("--admin-port must be a non-negative integer")
			}
			if n == 0 {
				f.adminEph = true
			} else {
				f.adminPort = n
			}
		case strings.HasPrefix(arg, "-"):
			return f, fmt.Errorf("unknown flag %q", arg)
		case f.file == "":
			f.file = arg
		default:
			return f, fmt.Errorf("unexpected argument %q", arg)
		}
		i++
	}

	if f.file == "" {
		return f, fmt.Errorf("serve requires a document path")
	}
	return f, nil
}

// parsePortList parses "name=port[,name=port...]" into dst.
func parsePortList(s string, dst map[string]int) error {
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			return fmt.Errorf("invalid port mapping %q (want name=port)", pair)
		}
		name := strings.TrimSpace(pair[:eq])
		port, err := strconv.Atoi(strings.TrimSpace(pair[eq+1:]))
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid port for %q: %s", name, pair[eq+1:])
		}
		dst[name] = port
	}
	return nil
}
