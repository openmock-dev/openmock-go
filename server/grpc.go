package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/openmock-dev/openmock-go/engine"
)

// newGRPCServer builds a grpc.Server that resolves every incoming call
// against the engine as the named document server. It uses a passthrough
// codec (messages stay raw []byte) and a single unknown-service handler, so
// one generic handler serves every method; the schema decodes and encodes the
// message bodies.
func newGRPCServer(eng *engine.Engine, serverName string, schema *grpcSchema) *grpc.Server {
	h := &grpcHandler{engine: eng, serverName: serverName, schema: schema}
	return grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(h.handle),
	)
}

type grpcHandler struct {
	engine     *engine.Engine
	serverName string
	schema     *grpcSchema
}

// handle resolves one gRPC call. It works for both unary and server-streaming
// operations: the client sends one request message, and the handler sends the
// engine's response message (unary) or messages (streaming) before returning
// the final status.
func (h *grpcHandler) handle(_ any, stream grpc.ServerStream) error {
	fullMethod, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "openmock: could not determine method")
	}
	service, rpc := splitMethod(fullMethod)

	req := &engine.Request{
		Protocol: "grpc",
		Server:   h.serverName,
		Service:  service,
		RPC:      rpc,
		Metadata: incomingMetadata(stream.Context()),
	}

	// Decode the request message when the method is known to the schema; an
	// unknown method needs no message (the engine returns unrouted).
	md, known := h.schema.method(service, rpc)
	if known {
		var raw rawMessage
		if err := stream.RecvMsg(&raw); err != nil {
			return err
		}
		msg, err := decodeMessage(md.Input(), raw)
		if err != nil {
			return status.Errorf(codes.Internal, "openmock: decoding request: %v", err)
		}
		req.Message = msg
	}

	resp := h.engine.Resolve(req)
	return h.writeResponse(stream, md, known, resp)
}

func (h *grpcHandler) writeResponse(stream grpc.ServerStream, md protoreflect.MethodDescriptor, known bool, resp map[string]any) error {
	if hdr := mdFrom(resp["metadata"]); hdr.Len() > 0 {
		stream.SetHeader(hdr)
	}
	if trailers := mdFrom(resp["trailers"]); trailers.Len() > 0 {
		stream.SetTrailer(trailers)
	}
	if d, ok := resp["delay"].(float64); ok && d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}

	// Messages are only sent for a routed method (the output type comes from
	// its descriptor); unrouted/unmatched responses carry only a status.
	if known {
		if msg, ok := resp["message"]; ok {
			if err := h.sendMessage(stream, md.Output(), msg); err != nil {
				return err
			}
		}
		if msgs, ok := resp["messages"].([]any); ok {
			for _, m := range msgs {
				if err := h.sendMessage(stream, md.Output(), m); err != nil {
					return err
				}
			}
		}
	}

	name, _ := resp["status"].(string)
	code := statusCode(name)
	if code == codes.OK {
		return nil
	}
	errMsg, _ := resp["error"].(string)
	return status.Error(code, errMsg)
}

func (h *grpcHandler) sendMessage(stream grpc.ServerStream, out protoreflect.MessageDescriptor, value any) error {
	raw, err := encodeMessage(out, value)
	if err != nil {
		return status.Errorf(codes.Internal, "openmock: encoding response: %v", err)
	}
	return stream.SendMsg(&raw)
}

// splitMethod turns "/package.Service/Method" into its service and rpc parts.
func splitMethod(fullMethod string) (service, rpc string) {
	trimmed := strings.TrimPrefix(fullMethod, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i], trimmed[i+1:]
	}
	return trimmed, ""
}

// incomingMetadata converts a call's incoming metadata into the engine's
// Metadata map: keys lowercased (gRPC already lowercases them) and repeated
// values joined with ", " (spec §4.6), mirroring the HTTP header rule.
func incomingMetadata(ctx context.Context) map[string]string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md) == 0 {
		return nil
	}
	out := make(map[string]string, len(md))
	for k, vals := range md {
		out[strings.ToLower(k)] = strings.Join(vals, ", ")
	}
	return out
}

// mdFrom builds gRPC metadata from the engine's metadata/trailers map (a
// map[string]any with string values).
func mdFrom(v any) metadata.MD {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(m)*2)
	for k, val := range m {
		if s, ok := val.(string); ok {
			pairs = append(pairs, k, s)
		}
	}
	return metadata.Pairs(pairs...)
}

// decodeMessage turns wire bytes into the engine's proto3-JSON value: raw
// protobuf -> dynamic message -> proto3 JSON -> any.
func decodeMessage(desc protoreflect.MessageDescriptor, raw rawMessage) (any, error) {
	msg := dynamicpb.NewMessage(desc)
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, err
	}
	jsonBytes, err := protojson.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(jsonBytes, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// encodeMessage turns the engine's rendered response value into wire bytes:
// any -> JSON -> dynamic message -> protobuf. Unknown fields are discarded so
// a lenient mock still serves a message carrying an extra field (spec §3.4).
func encodeMessage(desc protoreflect.MessageDescriptor, value any) (rawMessage, error) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(desc)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(jsonBytes, msg); err != nil {
		return nil, err
	}
	return proto.Marshal(msg)
}

// statusCode maps a canonical gRPC status name (spec §7.3) to its code. An
// unknown or empty name is treated as UNKNOWN.
func statusCode(name string) codes.Code {
	switch name {
	case "OK":
		return codes.OK
	case "CANCELLED":
		return codes.Canceled
	case "UNKNOWN":
		return codes.Unknown
	case "INVALID_ARGUMENT":
		return codes.InvalidArgument
	case "DEADLINE_EXCEEDED":
		return codes.DeadlineExceeded
	case "NOT_FOUND":
		return codes.NotFound
	case "ALREADY_EXISTS":
		return codes.AlreadyExists
	case "PERMISSION_DENIED":
		return codes.PermissionDenied
	case "RESOURCE_EXHAUSTED":
		return codes.ResourceExhausted
	case "FAILED_PRECONDITION":
		return codes.FailedPrecondition
	case "ABORTED":
		return codes.Aborted
	case "OUT_OF_RANGE":
		return codes.OutOfRange
	case "UNIMPLEMENTED":
		return codes.Unimplemented
	case "INTERNAL":
		return codes.Internal
	case "UNAVAILABLE":
		return codes.Unavailable
	case "DATA_LOSS":
		return codes.DataLoss
	case "UNAUTHENTICATED":
		return codes.Unauthenticated
	default:
		return codes.Unknown
	}
}

// rawMessage is a gRPC message left as raw wire bytes by rawCodec.
type rawMessage []byte

// rawCodec is a passthrough gRPC codec: it neither marshals nor unmarshals,
// leaving message bodies as the raw protobuf bytes on the wire. The adapter
// decodes and encodes them itself with the document's descriptor set, so it
// can serve any service without generated stubs.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	m, ok := v.(*rawMessage)
	if !ok {
		return nil, fmt.Errorf("openmock: rawCodec expects *rawMessage, got %T", v)
	}
	return *m, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	m, ok := v.(*rawMessage)
	if !ok {
		return fmt.Errorf("openmock: rawCodec expects *rawMessage, got %T", v)
	}
	*m = append((*m)[:0], data...)
	return nil
}

func (rawCodec) Name() string { return "openmock-raw" }
