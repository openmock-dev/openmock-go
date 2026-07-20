package engine

import "regexp"

// Document is a parsed and validated OpenMock document (spec §3). Use Parse
// to obtain one; the zero value is not usable.
type Document struct {
	OpenMock string
	Info     *Info
	Servers  []*Server
}

// Info is the document's optional, non-normative metadata (spec §3.2).
type Info struct {
	Title       string
	Version     string
	Description string
}

// ServerType is a server's protocol (spec §3.3).
type ServerType string

const (
	ServerHTTP      ServerType = "http"
	ServerGRPC      ServerType = "grpc"
	ServerGraphQL   ServerType = "graphql"
	ServerWebSocket ServerType = "websocket"
)

// Server is a named group of operations of one protocol (spec §3.3).
type Server struct {
	Name          string
	Type          ServerType
	Port          *int
	DescriptorSet string
	Schema        string
	Operations    []*Operation
}

// CallType is a gRPC operation's declared call shape (spec §5.4).
type CallType string

const (
	CallUnary           CallType = "unary"
	CallServerStreaming CallType = "server-streaming"
)

// Operation is one addressable route and its scenarios (spec §5). Which
// fields are populated depends on the owning Server's Type.
type Operation struct {
	// HTTP, WebSocket
	Method string // HTTP only
	Path   string // HTTP, WebSocket
	route  compiledPath

	// gRPC
	Service string
	RPC     string
	Type    CallType

	// GraphQL
	OperationType string
	OperationName string

	Summary   string
	Scenarios []*Scenario
}

// Scenario maps a matched request to a response (spec §6).
type Scenario struct {
	Name     string
	Summary  string
	When     *When
	Response *Response
}

// When declares the facets a request must satisfy for its Scenario to match
// (spec §6.1). A nil field means that facet is unconstrained.
type When struct {
	Params    map[string]FacetValue
	Query     map[string]QueryFacetValue
	Headers   map[string]FacetValue
	Body      *PayloadFacet
	Metadata  map[string]FacetValue
	Message   *PayloadFacet
	Variables map[string]FacetValue
	Calls     *CallsFacet
}

// FacetValue is a scalar facet value: a literal string to compare by
// canonical-string equality, or an exists/pattern matcher (spec §6.1).
type FacetValue struct {
	Kind       FacetValueKind
	Str        string
	Pattern    *regexp.Regexp
	PatternSrc string
}

// FacetValueKind discriminates the shape of a FacetValue.
type FacetValueKind int

const (
	FacetString FacetValueKind = iota
	FacetExistsTrue
	FacetExistsFalse
	FacetPattern
)

// PayloadFacet is the body/message facet's map-or-scalar form (spec §6.1).
type PayloadFacet struct {
	Scalar *string
	Map    map[string]FacetValue // dotted path -> value
}

// QueryFacetValue is one key's value under the query facet: either a
// whole-facet exists assertion, or an exact positional list of elements
// (spec §4.6, §6.1).
type QueryFacetValue struct {
	Exists   *bool
	Elements []QueryElement
}

// QueryElement is one position within a query facet's element list.
type QueryElement struct {
	IsPattern  bool
	Str        string
	Pattern    *regexp.Regexp
	PatternSrc string
}

// CallsFacet matches the operation's call counter against an inclusive
// range (spec §6.3). A nil bound is unconstrained on that side.
type CallsFacet struct {
	Min *int
	Max *int
}

// Response is the template for a scenario's answer (spec §7). Which fields
// apply depends on the owning Server's Type (and, for gRPC, the Operation's
// Type).
type Response struct {
	// HTTP
	Status  int
	Headers map[string]HeaderTemplate
	Body    any

	// gRPC
	GRPCStatus  string
	Error       string
	Metadata    map[string]string
	Trailers    map[string]string
	Message     any
	messageSet  bool
	Messages    []any
	messagesSet bool

	// GraphQL
	Data       any
	dataSet    bool
	Errors     []any
	Extensions any

	// WebSocket
	WSMessages    []any
	wsMessagesSet bool
	Close         *CloseSpec

	Delay int
}

// HeaderTemplate is a response header's value: one templated string, or a
// list of them emitted as separate field lines in order (spec §4.6, §7).
type HeaderTemplate struct {
	Values []string // len 1 for a plain string value
	IsList bool     // true when the document declared a YAML sequence
}

// CloseSpec is a WebSocket response's close directive (spec §7.5).
type CloseSpec struct {
	Code   int
	Reason string
}
