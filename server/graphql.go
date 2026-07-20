package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openmock-dev/openmock-go/engine"
)

// graphqlHandler is the GraphQL-over-HTTP adapter: an http.Handler bound to
// one GraphQL server that maps an incoming GraphQL request onto the engine's
// normalized request and the data/errors/extensions response back onto the
// wire. GraphQL messages are already JSON, so — unlike gRPC — no schema is
// needed to serve; a server's optional schema (spec §3.5) is not consulted.
//
// The whole port is the GraphQL endpoint: every path is accepted, since the
// OpenMock model addresses a GraphQL request by its server (port) and
// operation, not by URL path.
type graphqlHandler struct {
	engine     *engine.Engine
	serverName string
	// introspection answers __schema/__type queries from the server's schema
	// (spec §3.5); nil when the server has no usable schema.
	introspection *graphqlIntrospection
}

func newGraphQLHandler(eng *engine.Engine, serverName string, introspection *graphqlIntrospection) *graphqlHandler {
	return &graphqlHandler{engine: eng, serverName: serverName, introspection: introspection}
}

// gqlRequest is a GraphQL-over-HTTP request body.
type gqlRequest struct {
	Query         string `json:"query"`
	OperationName string `json:"operationName"`
	Variables     any    `json:"variables"`
}

func (h *graphqlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := h.readRequest(w, r)
	if !ok {
		return
	}

	// Introspection is answered from the schema, before the engine — it is an
	// added capability, not a change to how normal requests resolve (spec
	// §3.5). Only available when the server has a usable schema.
	if h.introspection != nil {
		if resp, isIntrospection := h.introspection.answer(req.Query, req.Variables); isIntrospection {
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// Derive the operation type and name from the query document (spec §4.3);
	// routing needs both. A query we cannot resolve to a single operation is a
	// bad request.
	opType, opName, err := selectOperation(req.Query, req.OperationName)
	if err != nil {
		writeGraphQLError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := h.engine.Resolve(&engine.Request{
		Protocol:      "graphql",
		Server:        h.serverName,
		OperationType: opType,
		OperationName: opName,
		Variables:     req.Variables,
		Headers:       normalizeHeaders(r.Header),
	})
	writeGraphQLResponse(w, resp)
}

// readRequest extracts the GraphQL request from a POST body (application/json)
// or a GET query string. It writes an error response and returns ok=false on a
// malformed or unsupported request.
func (h *graphqlHandler) readRequest(w http.ResponseWriter, r *http.Request) (gqlRequest, bool) {
	var req gqlRequest
	switch r.Method {
	case http.MethodPost:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeGraphQLError(w, http.StatusBadRequest, "could not read request body")
			return req, false
		}
		if err := json.Unmarshal(data, &req); err != nil {
			writeGraphQLError(w, http.StatusBadRequest, "invalid JSON request body")
			return req, false
		}
	case http.MethodGet:
		q := r.URL.Query()
		req.Query = q.Get("query")
		req.OperationName = q.Get("operationName")
		if v := q.Get("variables"); v != "" {
			// A malformed variables parameter simply yields no variables.
			_ = json.Unmarshal([]byte(v), &req.Variables)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		writeGraphQLError(w, http.StatusMethodNotAllowed, "method not allowed; use GET or POST")
		return req, false
	}
	if strings.TrimSpace(req.Query) == "" {
		writeGraphQLError(w, http.StatusBadRequest, "missing query")
		return req, false
	}
	return req, true
}

// writeGraphQLResponse renders the engine's GraphQL response (data / errors /
// extensions) as a 200 application/json body — GraphQL signals failure in-band
// through errors, not a transport status (spec §7.4).
func writeGraphQLResponse(w http.ResponseWriter, resp map[string]any) {
	if d, ok := resp["delay"].(float64); ok && d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	body := make(map[string]any, 3)
	for _, k := range []string{"data", "errors", "extensions"} {
		if v, ok := resp[k]; ok {
			body[k] = v
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(body)
}

// writeGraphQLError emits a transport-level failure (a malformed request the
// engine never sees) as a GraphQL errors-only body with the given HTTP status.
func writeGraphQLError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []any{map[string]any{"message": message}},
	})
}
