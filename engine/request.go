package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Request is a normalized incoming request (spec §4). Which fields apply
// depends on Protocol. Params is not part of the wire request: the engine
// fills it in during routing (spec §5.2).
type Request struct {
	Protocol string
	Server   string

	// HTTP, WebSocket
	Method  string
	Path    string
	Params  map[string]string
	Query   map[string][]string
	Headers map[string]string // keys lowercased; repeated values already joined with ", "

	// HTTP
	Body any

	// gRPC
	Service  string
	RPC      string
	Metadata map[string]string // keys lowercased; repeated values already joined with ", "
	Message  any

	// GraphQL
	OperationType string
	OperationName string
	Variables     any

	// WebSocket
	Connection string
}

// UnmarshalJSON decodes a normalized request per conformance/README.md's
// request.json shape, normalizing repeated wire values per spec §4.6:
// headers/metadata join with ", "; query stays a list.
func (r *Request) UnmarshalJSON(data []byte) error {
	var raw struct {
		Protocol      string                     `json:"protocol"`
		Server        string                     `json:"server"`
		Method        string                     `json:"method"`
		Path          string                     `json:"path"`
		Query         map[string]json.RawMessage `json:"query"`
		Headers       map[string]json.RawMessage `json:"headers"`
		Body          json.RawMessage            `json:"body"`
		Service       string                     `json:"service"`
		RPC           string                     `json:"rpc"`
		Metadata      map[string]json.RawMessage `json:"metadata"`
		Message       json.RawMessage            `json:"message"`
		OperationType string                     `json:"operationType"`
		OperationName string                     `json:"operationName"`
		Variables     json.RawMessage            `json:"variables"`
		Connection    string                     `json:"connection"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Protocol = raw.Protocol
	if r.Protocol == "" {
		r.Protocol = "http"
	}
	r.Server = raw.Server
	r.Method = strings.ToUpper(raw.Method)
	r.Path = raw.Path
	r.Service = raw.Service
	r.RPC = raw.RPC
	r.OperationType = raw.OperationType
	if r.OperationType == "" {
		r.OperationType = "query"
	}
	r.OperationName = raw.OperationName
	r.Connection = raw.Connection

	var err error
	if r.Query, err = decodeQueryMap(raw.Query); err != nil {
		return err
	}
	if r.Headers, err = decodeJoinedMap(raw.Headers); err != nil {
		return err
	}
	if r.Metadata, err = decodeJoinedMap(raw.Metadata); err != nil {
		return err
	}
	if len(raw.Body) > 0 {
		if err := json.Unmarshal(raw.Body, &r.Body); err != nil {
			return err
		}
	}
	if len(raw.Message) > 0 {
		if err := json.Unmarshal(raw.Message, &r.Message); err != nil {
			return err
		}
	}
	if len(raw.Variables) > 0 {
		if err := json.Unmarshal(raw.Variables, &r.Variables); err != nil {
			return err
		}
	}
	return nil
}

func decodeQueryMap(raw map[string]json.RawMessage) (map[string][]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := make(map[string][]string, len(raw))
	for k, v := range raw {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			out[k] = []string{s}
			continue
		}
		var list []string
		if err := json.Unmarshal(v, &list); err == nil {
			out[k] = list
			continue
		}
		return nil, fmt.Errorf("openmock: query value for %q must be a string or an array of strings", k)
	}
	return out, nil
}

func decodeJoinedMap(raw map[string]json.RawMessage) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			out[strings.ToLower(k)] = s
			continue
		}
		var list []string
		if err := json.Unmarshal(v, &list); err == nil {
			out[strings.ToLower(k)] = strings.Join(list, ", ")
			continue
		}
		return nil, fmt.Errorf("openmock: header/metadata value for %q must be a string or an array of strings", k)
	}
	return out, nil
}
