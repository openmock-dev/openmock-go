package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var supportedVersionPattern = regexp.MustCompile(`^0\.2\.\d+$`)

// Parse parses and validates an OpenMock document (spec §3). A document that
// violates any requirement of spec §3.6 is refused: Parse returns a non-nil
// error and no Document — never a partially usable one.
func Parse(data []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("openmock: invalid YAML: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("openmock: empty document")
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: document root must be a mapping")
	}
	doc, err := parseDocument(docNode)
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func mapPairs(n *yaml.Node) [][2]*yaml.Node {
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

func isExtensionKey(key string) bool {
	return strings.HasPrefix(key, "x-")
}

// ---- document ----

func parseDocument(n *yaml.Node) (*Document, error) {
	doc := &Document{}
	var serversNode *yaml.Node
	seenOpenmock := false
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch {
		case key == "openmock":
			seenOpenmock = true
			doc.OpenMock = val.Value
		case key == "info":
			info, err := parseInfo(val)
			if err != nil {
				return nil, err
			}
			doc.Info = info
		case key == "servers":
			serversNode = val
		case key == "operations":
			return nil, fmt.Errorf("openmock: line %d: top-level 'operations' is the 0.1 layout and is invalid in 0.2.0; declare a 'servers' list instead", kv[0].Line)
		case isExtensionKey(key):
			// specification extension; ignored
		default:
			return nil, fmt.Errorf("openmock: line %d: unknown top-level field %q", kv[0].Line, key)
		}
	}
	if !seenOpenmock {
		return nil, fmt.Errorf("openmock: missing required 'openmock' field")
	}
	if !supportedVersionPattern.MatchString(doc.OpenMock) {
		return nil, fmt.Errorf("openmock: unsupported openmock version %q; this implementation supports 0.2.x", doc.OpenMock)
	}
	if serversNode == nil {
		return nil, fmt.Errorf("openmock: missing required 'servers' field")
	}
	servers, err := parseServers(serversNode)
	if err != nil {
		return nil, err
	}
	doc.Servers = servers
	return doc, nil
}

func parseInfo(n *yaml.Node) (*Info, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: 'info' must be a mapping", n.Line)
	}
	info := &Info{}
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch {
		case key == "title":
			info.Title = val.Value
		case key == "version":
			info.Version = val.Value
		case key == "description":
			info.Description = val.Value
		case isExtensionKey(key):
		default:
			return nil, fmt.Errorf("openmock: line %d: unknown field %q in 'info'", kv[0].Line, key)
		}
	}
	return info, nil
}

// ---- servers ----

func parseServers(n *yaml.Node) ([]*Server, error) {
	if n.Kind != yaml.SequenceNode || len(n.Content) == 0 {
		return nil, fmt.Errorf("openmock: line %d: 'servers' must be a non-empty list", n.Line)
	}
	servers := make([]*Server, 0, len(n.Content))
	names := map[string]bool{}
	for _, item := range n.Content {
		s, err := parseServer(item)
		if err != nil {
			return nil, err
		}
		if names[s.Name] {
			return nil, fmt.Errorf("openmock: line %d: duplicate server name %q", item.Line, s.Name)
		}
		names[s.Name] = true
		servers = append(servers, s)
	}
	return servers, nil
}

func parseServer(n *yaml.Node) (*Server, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: a server must be a mapping", n.Line)
	}
	s := &Server{}
	var opsNode *yaml.Node
	seenName, seenType := false, false
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch {
		case key == "name":
			s.Name = val.Value
			seenName = true
		case key == "type":
			t := ServerType(val.Value)
			switch t {
			case ServerHTTP, ServerGRPC, ServerGraphQL, ServerWebSocket:
				s.Type = t
			default:
				return nil, fmt.Errorf("openmock: line %d: unknown server type %q", val.Line, val.Value)
			}
			seenType = true
		case key == "port":
			p, err := strconv.Atoi(val.Value)
			if err != nil {
				return nil, fmt.Errorf("openmock: line %d: 'port' must be an integer", val.Line)
			}
			s.Port = &p
		case key == "descriptorSet":
			s.DescriptorSet = val.Value
		case key == "schema":
			s.Schema = val.Value
		case key == "operations":
			opsNode = val
		case isExtensionKey(key):
		default:
			return nil, fmt.Errorf("openmock: line %d: unknown field %q on a server", kv[0].Line, key)
		}
	}
	if !seenName || s.Name == "" {
		return nil, fmt.Errorf("openmock: line %d: a server requires a non-empty 'name'", n.Line)
	}
	if !seenType {
		return nil, fmt.Errorf("openmock: line %d: server %q requires a 'type'", n.Line, s.Name)
	}
	if opsNode == nil || opsNode.Kind != yaml.SequenceNode || len(opsNode.Content) == 0 {
		return nil, fmt.Errorf("openmock: line %d: server %q requires a non-empty 'operations' list", n.Line, s.Name)
	}
	ops := make([]*Operation, 0, len(opsNode.Content))
	for _, item := range opsNode.Content {
		op, err := parseOperation(item, s.Type)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	s.Operations = ops
	return s, nil
}

// ---- operations ----

func parseOperation(n *yaml.Node, serverType ServerType) (*Operation, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: an operation must be a mapping", n.Line)
	}
	op := &Operation{Type: CallUnary, OperationType: "query"}
	var scenariosNode *yaml.Node
	seenMethod, seenPath, seenService, seenRPC, seenOperationName := false, false, false, false, false
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch {
		case key == "method" && (serverType == ServerHTTP):
			op.Method = val.Value
			seenMethod = true
		case key == "path" && (serverType == ServerHTTP || serverType == ServerWebSocket):
			if !strings.HasPrefix(val.Value, "/") {
				return nil, fmt.Errorf("openmock: line %d: 'path' must begin with '/'", val.Line)
			}
			op.Path = val.Value
			op.route = compilePath(val.Value)
			seenPath = true
		case key == "service" && serverType == ServerGRPC:
			op.Service = val.Value
			seenService = true
		case key == "rpc" && serverType == ServerGRPC:
			op.RPC = val.Value
			seenRPC = true
		case key == "type" && serverType == ServerGRPC:
			switch CallType(val.Value) {
			case CallUnary, CallServerStreaming:
				op.Type = CallType(val.Value)
			default:
				return nil, fmt.Errorf("openmock: line %d: unknown gRPC call type %q", val.Line, val.Value)
			}
		case key == "operationType" && serverType == ServerGraphQL:
			switch val.Value {
			case "query", "mutation":
				op.OperationType = val.Value
			default:
				return nil, fmt.Errorf("openmock: line %d: unknown GraphQL operationType %q", val.Line, val.Value)
			}
		case key == "operationName" && serverType == ServerGraphQL:
			op.OperationName = val.Value
			seenOperationName = true
		case key == "summary":
			op.Summary = val.Value
		case key == "scenarios":
			scenariosNode = val
		case isExtensionKey(key):
		default:
			return nil, fmt.Errorf("openmock: line %d: field %q is not valid on a %s operation", kv[0].Line, key, serverType)
		}
	}
	switch serverType {
	case ServerHTTP:
		if !seenMethod {
			return nil, fmt.Errorf("openmock: line %d: an HTTP operation requires 'method'", n.Line)
		}
		if !seenPath {
			return nil, fmt.Errorf("openmock: line %d: an HTTP operation requires 'path'", n.Line)
		}
	case ServerWebSocket:
		if !seenPath {
			return nil, fmt.Errorf("openmock: line %d: a WebSocket operation requires 'path'", n.Line)
		}
	case ServerGRPC:
		if !seenService {
			return nil, fmt.Errorf("openmock: line %d: a gRPC operation requires 'service'", n.Line)
		}
		if !seenRPC {
			return nil, fmt.Errorf("openmock: line %d: a gRPC operation requires 'rpc'", n.Line)
		}
	case ServerGraphQL:
		if !seenOperationName {
			return nil, fmt.Errorf("openmock: line %d: a GraphQL operation requires 'operationName'", n.Line)
		}
	}
	if scenariosNode == nil || scenariosNode.Kind != yaml.SequenceNode || len(scenariosNode.Content) == 0 {
		return nil, fmt.Errorf("openmock: line %d: an operation requires a non-empty 'scenarios' list", n.Line)
	}
	scenarios := make([]*Scenario, 0, len(scenariosNode.Content))
	for _, item := range scenariosNode.Content {
		sc, err := parseScenario(item, serverType, op.Type)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, sc)
	}
	op.Scenarios = scenarios
	return op, nil
}

// ---- scenarios ----

func parseScenario(n *yaml.Node, serverType ServerType, callType CallType) (*Scenario, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: a scenario must be a mapping", n.Line)
	}
	sc := &Scenario{}
	var responseNode *yaml.Node
	seenName, seenResponse := false, false
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch {
		case key == "name":
			sc.Name = val.Value
			seenName = true
		case key == "summary":
			sc.Summary = val.Value
		case key == "when":
			w, err := parseWhen(val, serverType)
			if err != nil {
				return nil, err
			}
			sc.When = w
		case key == "response":
			responseNode = val
			seenResponse = true
		case isExtensionKey(key):
		default:
			return nil, fmt.Errorf("openmock: line %d: unknown field %q on a scenario", kv[0].Line, key)
		}
	}
	if !seenName || sc.Name == "" {
		return nil, fmt.Errorf("openmock: line %d: a scenario requires a non-empty 'name'", n.Line)
	}
	if !seenResponse {
		return nil, fmt.Errorf("openmock: line %d: scenario %q requires a 'response'", n.Line, sc.Name)
	}
	resp, err := parseResponse(responseNode, serverType, callType)
	if err != nil {
		return nil, err
	}
	sc.Response = resp
	return sc, nil
}

// ---- when / facets ----

func facetAllowed(facet string, serverType ServerType) bool {
	switch facet {
	case "params", "query":
		return serverType == ServerHTTP || serverType == ServerWebSocket
	case "headers":
		return serverType == ServerHTTP || serverType == ServerGraphQL || serverType == ServerWebSocket
	case "body":
		return serverType == ServerHTTP
	case "metadata":
		return serverType == ServerGRPC
	case "message":
		return serverType == ServerGRPC || serverType == ServerWebSocket
	case "variables":
		return serverType == ServerGraphQL
	case "calls":
		return true
	}
	return false
}

func parseWhen(n *yaml.Node, serverType ServerType) (*When, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: 'when' must be a mapping", n.Line)
	}
	w := &When{}
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		if isExtensionKey(key) {
			continue
		}
		if !facetAllowed(key, serverType) {
			return nil, fmt.Errorf("openmock: line %d: facet %q is not valid under a %s operation", kv[0].Line, key, serverType)
		}
		var err error
		switch key {
		case "params":
			w.Params, err = parseFlatFacetMap(val, false)
		case "query":
			w.Query, err = parseQueryFacetMap(val)
		case "headers":
			w.Headers, err = parseFlatFacetMap(val, true)
		case "body":
			w.Body, err = parsePayloadFacet(val)
		case "metadata":
			w.Metadata, err = parseFlatFacetMap(val, true)
		case "message":
			w.Message, err = parsePayloadFacet(val)
		case "variables":
			w.Variables, err = parseFlatFacetMap(val, false)
		case "calls":
			w.Calls, err = parseCallsFacet(val)
		}
		if err != nil {
			return nil, err
		}
	}
	return w, nil
}

func parseFlatFacetMap(n *yaml.Node, foldCase bool) (map[string]FacetValue, error) {
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return nil, fmt.Errorf("openmock: line %d: a facet map must declare at least one key", n.Line)
	}
	out := map[string]FacetValue{}
	for _, kv := range mapPairs(n) {
		key := kv[0].Value
		if foldCase {
			key = strings.ToLower(key)
		}
		fv, err := parseFacetValue(kv[1])
		if err != nil {
			return nil, err
		}
		out[key] = fv
	}
	return out, nil
}

func parseFacetValue(n *yaml.Node) (FacetValue, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		s, ok := canonicalString(scalarValue(n))
		if !ok {
			return FacetValue{}, fmt.Errorf("openmock: line %d: facet value must be a string", n.Line)
		}
		return FacetValue{Kind: FacetString, Str: s}, nil
	case yaml.MappingNode:
		return parseMatcher(n)
	default:
		return FacetValue{}, fmt.Errorf("openmock: line %d: facet value must be a string or a matcher object", n.Line)
	}
}

func parseMatcher(n *yaml.Node) (FacetValue, error) {
	pairs := mapPairs(n)
	if len(pairs) != 1 {
		return FacetValue{}, fmt.Errorf("openmock: line %d: a matcher object must carry exactly one of 'exists' or 'pattern'", n.Line)
	}
	key, val := pairs[0][0].Value, pairs[0][1]
	switch key {
	case "exists":
		b, err := strconv.ParseBool(val.Value)
		if err != nil {
			return FacetValue{}, fmt.Errorf("openmock: line %d: 'exists' must be a boolean", val.Line)
		}
		if b {
			return FacetValue{Kind: FacetExistsTrue}, nil
		}
		return FacetValue{Kind: FacetExistsFalse}, nil
	case "pattern":
		re, err := compilePattern(val.Value)
		if err != nil {
			return FacetValue{}, fmt.Errorf("openmock: line %d: invalid pattern %q: %w", val.Line, val.Value, err)
		}
		return FacetValue{Kind: FacetPattern, Pattern: re, PatternSrc: val.Value}, nil
	default:
		return FacetValue{}, fmt.Errorf("openmock: line %d: unknown matcher %q", pairs[0][0].Line, key)
	}
}

func parseQueryFacetMap(n *yaml.Node) (map[string]QueryFacetValue, error) {
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return nil, fmt.Errorf("openmock: line %d: 'query' must declare at least one key", n.Line)
	}
	out := map[string]QueryFacetValue{}
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		qfv, err := parseQueryFacetValue(val)
		if err != nil {
			return nil, err
		}
		out[key] = qfv
	}
	return out, nil
}

func parseQueryFacetValue(n *yaml.Node) (QueryFacetValue, error) {
	switch n.Kind {
	case yaml.MappingNode:
		pairs := mapPairs(n)
		if len(pairs) != 1 || pairs[0][0].Value != "exists" {
			return QueryFacetValue{}, fmt.Errorf("openmock: line %d: a query facet value must be a list or an {exists: ...} matcher", n.Line)
		}
		b, err := strconv.ParseBool(pairs[0][1].Value)
		if err != nil {
			return QueryFacetValue{}, fmt.Errorf("openmock: line %d: 'exists' must be a boolean", pairs[0][1].Line)
		}
		return QueryFacetValue{Exists: &b}, nil
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			return QueryFacetValue{}, fmt.Errorf("openmock: line %d: a query facet list must be non-empty", n.Line)
		}
		elems := make([]QueryElement, 0, len(n.Content))
		for _, item := range n.Content {
			elem, err := parseQueryElement(item)
			if err != nil {
				return QueryFacetValue{}, err
			}
			elems = append(elems, elem)
		}
		return QueryFacetValue{Elements: elems}, nil
	default:
		return QueryFacetValue{}, fmt.Errorf("openmock: line %d: a query facet value must be a list or an {exists: ...} matcher", n.Line)
	}
}

func parseQueryElement(n *yaml.Node) (QueryElement, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		s, ok := canonicalString(scalarValue(n))
		if !ok {
			return QueryElement{}, fmt.Errorf("openmock: line %d: query element must be a string", n.Line)
		}
		return QueryElement{Str: s}, nil
	case yaml.MappingNode:
		pairs := mapPairs(n)
		if len(pairs) != 1 || pairs[0][0].Value != "pattern" {
			return QueryElement{}, fmt.Errorf("openmock: line %d: a query list element matcher may only be 'pattern'", n.Line)
		}
		re, err := compilePattern(pairs[0][1].Value)
		if err != nil {
			return QueryElement{}, fmt.Errorf("openmock: line %d: invalid pattern %q: %w", pairs[0][1].Line, pairs[0][1].Value, err)
		}
		return QueryElement{IsPattern: true, Pattern: re, PatternSrc: pairs[0][1].Value}, nil
	default:
		return QueryElement{}, fmt.Errorf("openmock: line %d: invalid query element", n.Line)
	}
}

func parsePayloadFacet(n *yaml.Node) (*PayloadFacet, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		s, ok := canonicalString(scalarValue(n))
		if !ok {
			return nil, fmt.Errorf("openmock: line %d: scalar facet value must be a string", n.Line)
		}
		return &PayloadFacet{Scalar: &s}, nil
	case yaml.MappingNode:
		if len(n.Content) == 0 {
			return nil, fmt.Errorf("openmock: line %d: a facet map must declare at least one key", n.Line)
		}
		m := map[string]FacetValue{}
		for _, kv := range mapPairs(n) {
			fv, err := parseFacetValue(kv[1])
			if err != nil {
				return nil, err
			}
			m[kv[0].Value] = fv
		}
		return &PayloadFacet{Map: m}, nil
	default:
		return nil, fmt.Errorf("openmock: line %d: a body/message facet must be a string or a mapping", n.Line)
	}
}

func parseCallsFacet(n *yaml.Node) (*CallsFacet, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		v, err := strconv.Atoi(n.Value)
		if err != nil || v < 1 {
			return nil, fmt.Errorf("openmock: line %d: 'calls' must be a positive integer", n.Line)
		}
		return &CallsFacet{Min: &v, Max: intPtr(v)}, nil
	case yaml.MappingNode:
		cf := &CallsFacet{}
		seen := false
		for _, kv := range mapPairs(n) {
			key, val := kv[0].Value, kv[1]
			switch key {
			case "min":
				v, err := strconv.Atoi(val.Value)
				if err != nil || v < 1 {
					return nil, fmt.Errorf("openmock: line %d: 'calls.min' must be a positive integer", val.Line)
				}
				cf.Min = &v
				seen = true
			case "max":
				v, err := strconv.Atoi(val.Value)
				if err != nil || v < 1 {
					return nil, fmt.Errorf("openmock: line %d: 'calls.max' must be a positive integer", val.Line)
				}
				cf.Max = &v
				seen = true
			default:
				return nil, fmt.Errorf("openmock: line %d: unknown field %q on 'calls'", kv[0].Line, key)
			}
		}
		if !seen {
			return nil, fmt.Errorf("openmock: line %d: 'calls' requires at least one of 'min'/'max'", n.Line)
		}
		if cf.Min != nil && cf.Max != nil && *cf.Min > *cf.Max {
			return nil, fmt.Errorf("openmock: line %d: 'calls' range is inverted (min %d > max %d)", n.Line, *cf.Min, *cf.Max)
		}
		return cf, nil
	default:
		return nil, fmt.Errorf("openmock: line %d: 'calls' must be an integer or an object", n.Line)
	}
}

func intPtr(v int) *int { return &v }

// ---- response ----

var grpcStatusCodes = map[string]bool{
	"OK": true, "CANCELLED": true, "UNKNOWN": true, "INVALID_ARGUMENT": true,
	"DEADLINE_EXCEEDED": true, "NOT_FOUND": true, "ALREADY_EXISTS": true,
	"PERMISSION_DENIED": true, "RESOURCE_EXHAUSTED": true, "FAILED_PRECONDITION": true,
	"ABORTED": true, "OUT_OF_RANGE": true, "UNIMPLEMENTED": true, "INTERNAL": true,
	"UNAVAILABLE": true, "DATA_LOSS": true, "UNAUTHENTICATED": true,
}

func parseResponse(n *yaml.Node, serverType ServerType, callType CallType) (*Response, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: a response must be a mapping", n.Line)
	}
	r := &Response{}
	seenStatus := false
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		if isExtensionKey(key) {
			continue
		}
		if key == "delay" {
			d, err := strconv.Atoi(val.Value)
			if err != nil || d < 0 {
				return nil, fmt.Errorf("openmock: line %d: 'delay' must be a non-negative integer", val.Line)
			}
			r.Delay = d
			continue
		}
		switch serverType {
		case ServerHTTP:
			switch key {
			case "status":
				s, err := strconv.Atoi(val.Value)
				if err != nil {
					return nil, fmt.Errorf("openmock: line %d: 'status' must be an integer", val.Line)
				}
				r.Status = s
				seenStatus = true
			case "headers":
				hm, err := parseHeaderMap(val)
				if err != nil {
					return nil, err
				}
				r.Headers = hm
			case "body":
				r.Body = nodeToValue(val)
			default:
				return nil, fmt.Errorf("openmock: line %d: field %q is not valid on an HTTP response", kv[0].Line, key)
			}
		case ServerGRPC:
			switch key {
			case "status":
				if !grpcStatusCodes[val.Value] {
					return nil, fmt.Errorf("openmock: line %d: unknown gRPC status code %q", val.Line, val.Value)
				}
				r.GRPCStatus = val.Value
				seenStatus = true
			case "error":
				r.Error = val.Value
			case "metadata":
				m, err := parseGRPCStringMap(val)
				if err != nil {
					return nil, err
				}
				r.Metadata = m
			case "trailers":
				m, err := parseGRPCStringMap(val)
				if err != nil {
					return nil, err
				}
				r.Trailers = m
			case "message":
				if callType != CallUnary {
					return nil, fmt.Errorf("openmock: line %d: 'message' is only valid on a unary operation's response; use 'messages'", val.Line)
				}
				r.Message = nodeToValue(val)
				r.messageSet = true
			case "messages":
				if callType != CallServerStreaming {
					return nil, fmt.Errorf("openmock: line %d: 'messages' is only valid on a server-streaming operation's response; use 'message'", val.Line)
				}
				v := nodeToValue(val)
				list, _ := v.([]any)
				r.Messages = list
				r.messagesSet = true
			default:
				return nil, fmt.Errorf("openmock: line %d: field %q is not valid on a gRPC response", kv[0].Line, key)
			}
		case ServerGraphQL:
			switch key {
			case "data":
				r.Data = nodeToValue(val)
				r.dataSet = true
			case "errors":
				v := nodeToValue(val)
				list, _ := v.([]any)
				r.Errors = list
			case "extensions":
				r.Extensions = nodeToValue(val)
			default:
				return nil, fmt.Errorf("openmock: line %d: field %q is not valid on a GraphQL response", kv[0].Line, key)
			}
		case ServerWebSocket:
			switch key {
			case "messages":
				v := nodeToValue(val)
				list, _ := v.([]any)
				r.WSMessages = list
				r.wsMessagesSet = true
			case "close":
				cs, err := parseCloseSpec(val)
				if err != nil {
					return nil, err
				}
				r.Close = cs
			default:
				return nil, fmt.Errorf("openmock: line %d: field %q is not valid on a WebSocket response", kv[0].Line, key)
			}
		}
	}

	switch serverType {
	case ServerHTTP:
		if !seenStatus {
			return nil, fmt.Errorf("openmock: line %d: an HTTP response requires 'status'", n.Line)
		}
	case ServerGRPC:
		if !seenStatus {
			return nil, fmt.Errorf("openmock: line %d: a gRPC response requires 'status'", n.Line)
		}
		if r.messageSet && r.GRPCStatus != "OK" {
			return nil, fmt.Errorf("openmock: line %d: a unary gRPC response with a non-OK status MUST NOT carry a 'message'", n.Line)
		}
	case ServerGraphQL:
		if !r.dataSet && len(r.Errors) == 0 {
			return nil, fmt.Errorf("openmock: line %d: a GraphQL response requires at least one of 'data'/'errors'", n.Line)
		}
	case ServerWebSocket:
		if !r.wsMessagesSet && r.Close == nil {
			return nil, fmt.Errorf("openmock: line %d: a WebSocket response requires at least one of 'messages'/'close'", n.Line)
		}
	}
	return r, nil
}

func parseHeaderMap(n *yaml.Node) (map[string]HeaderTemplate, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: 'headers' must be a mapping", n.Line)
	}
	out := map[string]HeaderTemplate{}
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch val.Kind {
		case yaml.ScalarNode:
			out[key] = HeaderTemplate{Values: []string{val.Value}}
		case yaml.SequenceNode:
			vals := make([]string, len(val.Content))
			for i, item := range val.Content {
				if item.Kind != yaml.ScalarNode {
					return nil, fmt.Errorf("openmock: line %d: header list items must be strings", item.Line)
				}
				vals[i] = item.Value
			}
			out[key] = HeaderTemplate{Values: vals, IsList: true}
		default:
			return nil, fmt.Errorf("openmock: line %d: a header value must be a string or a list of strings", val.Line)
		}
	}
	return out, nil
}

func parseGRPCStringMap(n *yaml.Node) (map[string]string, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: expected a mapping", n.Line)
	}
	out := map[string]string{}
	for _, kv := range mapPairs(n) {
		if kv[1].Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("openmock: line %d: value must be a string", kv[1].Line)
		}
		out[kv[0].Value] = kv[1].Value
	}
	return out, nil
}

func parseCloseSpec(n *yaml.Node) (*CloseSpec, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openmock: line %d: 'close' must be a mapping", n.Line)
	}
	cs := &CloseSpec{Code: 1000}
	for _, kv := range mapPairs(n) {
		key, val := kv[0].Value, kv[1]
		switch key {
		case "code":
			c, err := strconv.Atoi(val.Value)
			if err != nil {
				return nil, fmt.Errorf("openmock: line %d: 'close.code' must be an integer", val.Line)
			}
			cs.Code = c
		case "reason":
			cs.Reason = val.Value
		default:
			return nil, fmt.Errorf("openmock: line %d: unknown field %q on 'close'", kv[0].Line, key)
		}
	}
	return cs, nil
}

// ---- node -> generic value ----

func nodeToValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for _, kv := range mapPairs(n) {
			m[kv[0].Value] = nodeToValue(kv[1])
		}
		return m
	case yaml.SequenceNode:
		s := make([]any, len(n.Content))
		for i, c := range n.Content {
			s[i] = nodeToValue(c)
		}
		return s
	case yaml.ScalarNode:
		return scalarValue(n)
	case yaml.AliasNode:
		return nodeToValue(n.Alias)
	default:
		return nil
	}
}

func scalarValue(n *yaml.Node) any {
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		b, err := strconv.ParseBool(n.Value)
		if err != nil {
			return n.Value
		}
		return b
	case "!!int":
		i, err := strconv.ParseInt(n.Value, 0, 64)
		if err != nil {
			if f, ferr := strconv.ParseFloat(n.Value, 64); ferr == nil {
				return f
			}
			return n.Value
		}
		return float64(i)
	case "!!float":
		f, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return n.Value
		}
		return f
	default:
		// !!str and anything else (e.g. an implicit !!timestamp) keep their
		// literal source text.
		return n.Value
	}
}

// ---- cross-document validation ----

func validateDocument(doc *Document) error {
	for _, s := range doc.Servers {
		for _, op := range s.Operations {
			for _, sc := range op.Scenarios {
				if err := validateResponseFakers(sc.Response); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateResponseFakers(r *Response) error {
	check := func(s string) error {
		for _, ph := range scanPlaceholders(s) {
			if isQuotedLiteral(ph.expr) {
				continue
			}
			if strings.HasPrefix(ph.expr, "faker.") {
				rest := ph.expr[len("faker."):]
				parts := strings.SplitN(rest, ".", 2)
				if len(parts) != 2 || !knownFaker(parts[0], parts[1]) {
					return fmt.Errorf("openmock: unknown faker placeholder %q", ph.expr)
				}
			}
		}
		return nil
	}
	for _, h := range r.Headers {
		for _, v := range h.Values {
			if err := check(v); err != nil {
				return err
			}
		}
	}
	for _, v := range r.Metadata {
		if err := check(v); err != nil {
			return err
		}
	}
	for _, v := range r.Trailers {
		if err := check(v); err != nil {
			return err
		}
	}
	if err := check(r.Error); err != nil {
		return err
	}
	if r.Close != nil {
		if err := check(r.Close.Reason); err != nil {
			return err
		}
	}
	if err := walkValueStrings(r.Body, check); err != nil {
		return err
	}
	if err := walkValueStrings(r.Message, check); err != nil {
		return err
	}
	for _, m := range r.Messages {
		if err := walkValueStrings(m, check); err != nil {
			return err
		}
	}
	if err := walkValueStrings(r.Data, check); err != nil {
		return err
	}
	for _, e := range r.Errors {
		if err := walkValueStrings(e, check); err != nil {
			return err
		}
	}
	if err := walkValueStrings(r.Extensions, check); err != nil {
		return err
	}
	for _, m := range r.WSMessages {
		if err := walkValueStrings(m, check); err != nil {
			return err
		}
	}
	return nil
}

func walkValueStrings(v any, fn func(string) error) error {
	switch t := v.(type) {
	case string:
		return fn(t)
	case map[string]any:
		for _, vv := range t {
			if err := walkValueStrings(vv, fn); err != nil {
				return err
			}
		}
	case []any:
		for _, vv := range t {
			if err := walkValueStrings(vv, fn); err != nil {
				return err
			}
		}
	}
	return nil
}
