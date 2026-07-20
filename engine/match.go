package engine

import (
	"fmt"
	"strings"
	"sync"
)

// Engine resolves normalized requests against a parsed Document, maintaining
// the per-operation call counters the calls facet reads (spec §6.3). The
// zero value is not usable; use New.
//
// An Engine is safe for concurrent use: a serving adapter may call Resolve
// from many goroutines at once (one per in-flight request). The call
// counters are the only mutable state, and mu guards them — a plain map
// increment would both race and, worse, trigger Go's fatal "concurrent map
// writes" panic under real load.
type Engine struct {
	doc *Document

	mu       sync.Mutex
	counters map[string]int
}

// New creates an Engine for doc, with fresh call counters.
func New(doc *Document) *Engine {
	return &Engine{doc: doc, counters: map[string]int{}}
}

// Resolve routes req, advances the matched operation's call counter, selects
// the first matching scenario, and renders its response (spec §8). The
// result is ready to marshal as the protocol's normalized response (see
// conformance/README.md's expected.json shapes). An unrouted or unmatched
// request yields the synthetic response of spec §10.
func (e *Engine) Resolve(req *Request) map[string]any {
	server, op, params, ok := e.route(req)
	if !ok {
		return unroutedResponse(req)
	}
	req.Params = params

	key := counterKey(server, op, req)
	e.mu.Lock()
	e.counters[key]++
	calls := e.counters[key]
	e.mu.Unlock()

	scenario := selectScenario(op, req, calls)
	if scenario == nil {
		return unmatchedResponse(server, op, req)
	}
	return render(server, op, scenario.Response, req)
}

func (e *Engine) route(req *Request) (*Server, *Operation, map[string]string, bool) {
	server := e.targetServer(req)
	if server == nil {
		return nil, nil, nil, false
	}
	switch server.Type {
	case ServerHTTP:
		for _, op := range server.Operations {
			if !strings.EqualFold(op.Method, req.Method) {
				continue
			}
			if params, ok := op.route.match(req.Path); ok {
				return server, op, params, true
			}
		}
	case ServerGRPC:
		for _, op := range server.Operations {
			if op.Service == req.Service && op.RPC == req.RPC {
				return server, op, nil, true
			}
		}
	case ServerGraphQL:
		for _, op := range server.Operations {
			if op.OperationType == req.OperationType && op.OperationName == req.OperationName {
				return server, op, nil, true
			}
		}
	case ServerWebSocket:
		for _, op := range server.Operations {
			if params, ok := op.route.match(req.Path); ok {
				return server, op, params, true
			}
		}
	}
	return nil, nil, nil, false
}

// targetServer selects the server req addresses (spec §4, §8 step 1): the
// named server if req.Server is set (and its type matches the request's
// protocol), otherwise the document's sole server of that protocol.
func (e *Engine) targetServer(req *Request) *Server {
	if req.Server != "" {
		for _, s := range e.doc.Servers {
			if s.Name == req.Server {
				if string(s.Type) != req.Protocol {
					return nil
				}
				return s
			}
		}
		return nil
	}
	var sole *Server
	count := 0
	for _, s := range e.doc.Servers {
		if string(s.Type) == req.Protocol {
			sole = s
			count++
		}
	}
	if count == 1 {
		return sole
	}
	return nil
}

// counterKey identifies the (server, operation, concrete address) tuple the
// call counter is keyed by (spec §6.3).
func counterKey(server *Server, op *Operation, req *Request) string {
	switch server.Type {
	case ServerHTTP:
		return fmt.Sprintf("%s|%s %s", server.Name, strings.ToUpper(req.Method), req.Path)
	case ServerGRPC:
		return fmt.Sprintf("%s|%s/%s", server.Name, op.Service, op.RPC)
	case ServerGraphQL:
		return fmt.Sprintf("%s|%s %s", server.Name, op.OperationType, op.OperationName)
	case ServerWebSocket:
		return fmt.Sprintf("%s|%s|%s", server.Name, req.Path, req.Connection)
	}
	return ""
}

func selectScenario(op *Operation, req *Request, calls int) *Scenario {
	for _, sc := range op.Scenarios {
		if sc.When == nil {
			return sc
		}
		if matchWhen(sc.When, req, calls) {
			return sc
		}
	}
	return nil
}

func matchWhen(w *When, req *Request, calls int) bool {
	if w.Params != nil && !matchFlatFacet(w.Params, req.Params) {
		return false
	}
	if w.Query != nil && !matchQueryFacet(w.Query, req.Query) {
		return false
	}
	if w.Headers != nil && !matchFlatFacet(w.Headers, req.Headers) {
		return false
	}
	if w.Body != nil && !matchPayloadFacet(w.Body, req.Body) {
		return false
	}
	if w.Metadata != nil && !matchFlatFacet(w.Metadata, req.Metadata) {
		return false
	}
	if w.Message != nil && !matchPayloadFacet(w.Message, req.Message) {
		return false
	}
	if w.Variables != nil && !matchDottedFacet(w.Variables, req.Variables) {
		return false
	}
	if w.Calls != nil && !matchCalls(w.Calls, calls) {
		return false
	}
	return true
}

func matchFlatFacet(facet map[string]FacetValue, actual map[string]string) bool {
	for k, fv := range facet {
		av, ok := actual[k]
		if !matchFacetValue(fv, av, ok) {
			return false
		}
	}
	return true
}

func matchFacetValue(fv FacetValue, actual string, present bool) bool {
	switch fv.Kind {
	case FacetExistsTrue:
		return present
	case FacetExistsFalse:
		return !present
	case FacetPattern:
		return present && fv.Pattern.MatchString(actual)
	default:
		return present && actual == fv.Str
	}
}

func matchQueryFacet(facet map[string]QueryFacetValue, actual map[string][]string) bool {
	for k, fv := range facet {
		av, present := actual[k]
		if fv.Exists != nil {
			if *fv.Exists != present {
				return false
			}
			continue
		}
		if !present || len(av) != len(fv.Elements) {
			return false
		}
		for i, elem := range fv.Elements {
			if elem.IsPattern {
				if !elem.Pattern.MatchString(av[i]) {
					return false
				}
			} else if av[i] != elem.Str {
				return false
			}
		}
	}
	return true
}

func matchPayloadFacet(pf *PayloadFacet, payload any) bool {
	if pf.Scalar != nil {
		s, ok := canonicalString(payload)
		return ok && s == *pf.Scalar
	}
	return matchDottedFacet(pf.Map, payload)
}

func matchDottedFacet(facet map[string]FacetValue, payload any) bool {
	for path, fv := range facet {
		v, ok := resolveDottedPath(payload, path)
		if ok && v == nil {
			ok = false // null behaves exactly like absent (spec §4.5)
		}
		if !matchDottedFacetValue(fv, v, ok) {
			return false
		}
	}
	return true
}

func matchDottedFacetValue(fv FacetValue, v any, resolved bool) bool {
	switch fv.Kind {
	case FacetExistsTrue:
		return resolved
	case FacetExistsFalse:
		return !resolved
	case FacetPattern:
		if !resolved {
			return false
		}
		s, ok := canonicalString(v)
		return ok && fv.Pattern.MatchString(s)
	default:
		if !resolved {
			return false
		}
		s, ok := canonicalString(v)
		return ok && s == fv.Str
	}
}

func matchCalls(cf *CallsFacet, calls int) bool {
	if cf.Min != nil && calls < *cf.Min {
		return false
	}
	if cf.Max != nil && calls > *cf.Max {
		return false
	}
	return true
}

// unroutedResponse builds the synthetic response for spec §10.2: no
// operation matched the request.
func unroutedResponse(req *Request) map[string]any {
	switch req.Protocol {
	case "grpc":
		return map[string]any{
			"status": "UNIMPLEMENTED",
			"error":  "No operation matched the request.",
			"trailers": map[string]any{
				"openmock":         "unrouted",
				"openmock-service": req.Service,
				"openmock-rpc":     req.RPC,
			},
		}
	case "graphql":
		return map[string]any{
			"errors": []any{
				map[string]any{
					"message": "No operation matched the request.",
					"extensions": map[string]any{
						"openmock":      "unrouted",
						"operationType": req.OperationType,
						"operationName": req.OperationName,
					},
				},
			},
		}
	case "websocket":
		return map[string]any{
			"status": float64(404),
			"body": map[string]any{
				"openmock": "unrouted",
				"message":  "No operation matched the request.",
				"path":     req.Path,
			},
		}
	default: // http
		return map[string]any{
			"status": float64(404),
			"body": map[string]any{
				"openmock": "unrouted",
				"message":  "No operation matched the request.",
				"method":   req.Method,
				"path":     req.Path,
			},
		}
	}
}

// unmatchedResponse builds the synthetic response for spec §10.1: the
// request routed, but no scenario matched and there was no default.
func unmatchedResponse(server *Server, op *Operation, req *Request) map[string]any {
	switch server.Type {
	case ServerGRPC:
		return map[string]any{
			"status": "UNIMPLEMENTED",
			"error":  "No scenario matched and no default scenario is defined.",
			"trailers": map[string]any{
				"openmock":         "unmatched",
				"openmock-service": op.Service,
				"openmock-rpc":     op.RPC,
			},
		}
	case ServerGraphQL:
		return map[string]any{
			"errors": []any{
				map[string]any{
					"message": "No scenario matched and no default scenario is defined.",
					"extensions": map[string]any{
						"openmock":      "unmatched",
						"operationType": op.OperationType,
						"operationName": op.OperationName,
					},
				},
			},
		}
	case ServerWebSocket:
		return map[string]any{
			"messages": []any{
				map[string]any{
					"openmock": "unmatched",
					"message":  "No scenario matched and no default scenario is defined.",
					"path":     req.Path,
				},
			},
		}
	default: // http
		return map[string]any{
			"status": float64(501),
			"body": map[string]any{
				"openmock": "unmatched",
				"message":  "No scenario matched and no default scenario is defined.",
				"method":   req.Method,
				"path":     req.Path,
			},
		}
	}
}
