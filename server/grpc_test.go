package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/openmock-dev/openmock-go/engine"
)

// The gRPC tests serve a document backed by testdata/shop.binpb (the schema
// from examples/08-grpc.proto) and drive it with a real gRPC client that
// builds request/response messages dynamically from the same descriptor — no
// generated stubs. The client uses the same raw passthrough codec as the
// server, so requests and responses cross the wire as real protobuf.

const grpcDoc = `
openmock: 0.2.0
servers:
  - name: main
    type: grpc
    descriptorSet: shop.binpb
    operations:
      - service: shop.v1.OrderService
        rpc: GetOrder
        type: unary
        scenarios:
          - name: admin-view
            when:
              metadata:
                x-role: admin
            response:
              status: OK
              metadata:
                x-cache: MISS
              message:
                id: "{{request.message.orderId}}"
                state: SHIPPED
          - name: missing
            when:
              message:
                orderId: "0"
            response:
              status: NOT_FOUND
              error: "order {{request.message.orderId}} not found"
          - name: default
            response:
              status: OK
              message:
                id: "{{request.message.orderId}}"
                state: SHIPPED
      - service: shop.v1.OrderService
        rpc: WatchOrder
        type: server-streaming
        scenarios:
          - name: lifecycle
            response:
              status: OK
              messages:
                - event: CREATED
                  orderId: "{{request.message.orderId}}"
                - event: PAID
                  orderId: "{{request.message.orderId}}"
                - event: SHIPPED
                  orderId: "{{request.message.orderId}}"
      - service: shop.v1.ReportService
        rpc: GetReport
        type: unary
        scenarios:
          - name: generating
            when:
              calls: {max: 1}
            response:
              status: OK
              message:
                state: GENERATING
          - name: ready
            response:
              status: OK
              message:
                state: READY
`

// grpcFixture serves grpcDoc on a free port and returns a dialed client
// connection plus the loaded schema (for building messages).
func grpcFixture(t *testing.T) (*grpc.ClientConn, *grpcSchema) {
	t.Helper()
	doc, err := engine.Parse([]byte(grpcDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	port := freeTCPPort(t)
	srv, err := Serve(doc, Options{
		AdminPortEphemeral: true,
		Ports:              map[string]int{"main": port},
		BaseDir:            "testdata",
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Wait()
	})

	schema, err := loadDescriptor("testdata", &engine.Server{Name: "main", Type: engine.ServerGRPC, DescriptorSet: "shop.binpb"})
	if err != nil {
		t.Fatalf("loadDescriptor: %v", err)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	return cc, schema
}

func TestGRPCUnary(t *testing.T) {
	cc, schema := grpcFixture(t)
	md, _ := schema.method("shop.v1.OrderService", "GetOrder")

	// Default scenario: templated id from the request, enum name preserved.
	got, hdr, _, err := invokeUnary(t, cc, md, "shop.v1.OrderService", "GetOrder", `{"orderId":"42"}`, nil)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got["id"] != "42" || got["state"] != "SHIPPED" {
		t.Errorf("message = %v, want id 42 / state SHIPPED", got)
	}
	_ = hdr

	// Metadata match: x-role admin selects the scenario that sets x-cache.
	_, hdr, _, err = invokeUnary(t, cc, md, "shop.v1.OrderService", "GetOrder", `{"orderId":"7"}`,
		metadata.Pairs("x-role", "admin"))
	if err != nil {
		t.Fatalf("GetOrder admin: %v", err)
	}
	if got := hdr.Get("x-cache"); len(got) != 1 || got[0] != "MISS" {
		t.Errorf("x-cache header = %v, want [MISS]", got)
	}
}

func TestGRPCErrorStatus(t *testing.T) {
	cc, schema := grpcFixture(t)
	md, _ := schema.method("shop.v1.OrderService", "GetOrder")

	// orderId "0" selects the NOT_FOUND scenario with a templated error and
	// no message.
	_, _, _, err := invokeUnary(t, cc, md, "shop.v1.OrderService", "GetOrder", `{"orderId":"0"}`, nil)
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Fatalf("status = %v, want NOT_FOUND", err)
	}
	if st.Message() != "order 0 not found" {
		t.Errorf("error = %q, want 'order 0 not found'", st.Message())
	}
}

func TestGRPCServerStreaming(t *testing.T) {
	cc, schema := grpcFixture(t)
	md, _ := schema.method("shop.v1.OrderService", "WatchOrder")

	msgs, err := invokeStream(t, cc, md, "shop.v1.OrderService", "WatchOrder", `{"orderId":"42"}`)
	if err != nil {
		t.Fatalf("WatchOrder: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	wantEvents := []string{"CREATED", "PAID", "SHIPPED"}
	for i, m := range msgs {
		if m["event"] != wantEvents[i] || m["orderId"] != "42" {
			t.Errorf("message %d = %v, want event %s / orderId 42", i, m, wantEvents[i])
		}
	}
}

func TestGRPCUnrouted(t *testing.T) {
	cc, schema := grpcFixture(t)
	md, _ := schema.method("shop.v1.OrderService", "GetOrder") // any known type for codec plumbing

	// An unknown method: UNIMPLEMENTED with identifying trailers.
	var tlr metadata.MD
	_, _, tlr, err := invokeUnary(t, cc, md, "shop.v1.OrderService", "NoSuchRPC", `{}`, nil)
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unimplemented {
		t.Fatalf("status = %v, want UNIMPLEMENTED", err)
	}
	if got := tlr.Get("openmock"); len(got) != 1 || got[0] != "unrouted" {
		t.Errorf("openmock trailer = %v, want [unrouted]", got)
	}
	if got := tlr.Get("openmock-rpc"); len(got) != 1 || got[0] != "NoSuchRPC" {
		t.Errorf("openmock-rpc trailer = %v, want [NoSuchRPC]", got)
	}
}

// TestGRPCSoleServerDefaultPort pins that a document's sole gRPC server gets
// the stable default 3003 (docs/serving.md §2), without binding it.
func TestGRPCSoleServerDefaultPort(t *testing.T) {
	doc, err := engine.Parse([]byte(grpcDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolved, err := resolvePorts(doc, nil, defaultAdminPort)
	if err != nil {
		t.Fatalf("resolvePorts: %v", err)
	}
	if len(resolved) != 1 || resolved[0].port != defaultGRPCPort || resolved[0].source != sourceDefault {
		t.Fatalf("resolved = %+v, want grpc 'main' on 3003 from sourceDefault", resolved)
	}
}

// TestGRPCWithoutDescriptorUnboundWithNote pins the descriptorSet visibility
// contract: the document is valid and Serve succeeds either way (spec §3.4 —
// the field never changes what a document means), but a gRPC server without a
// loadable descriptor is not bound, and the admin topology entry says why via
// its note field (null transport/port/url, per docs/serving.md §3).
func TestGRPCWithoutDescriptorUnboundWithNote(t *testing.T) {
	cases := []struct {
		name       string
		descriptor string // "" = field absent; else an unreadable path
		wantInNote string
	}{
		{"no descriptorSet", "", "requires a descriptorSet"},
		{"unreadable descriptorSet", "does-not-exist.binpb", "could not be loaded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			descriptorLine := ""
			if tc.descriptor != "" {
				descriptorLine = "\n    descriptorSet: " + tc.descriptor
			}
			doc, err := engine.Parse([]byte(`
openmock: 0.2.0
servers:
  - name: main
    type: grpc` + descriptorLine + `
    operations:
      - service: shop.v1.OrderService
        rpc: GetOrder
        type: unary
        scenarios:
          - name: ok
            response:
              status: OK
              message: {ok: true}
`))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			srv, err := Serve(doc, Options{AdminPortEphemeral: true, BaseDir: "testdata"})
			if err != nil {
				t.Fatalf("Serve must succeed with an unbound grpc server, got: %v", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				srv.Shutdown(ctx)
				srv.Wait()
			}()

			top := srv.Topology()
			if len(top) != 1 {
				t.Fatalf("topology = %+v, want one entry", top)
			}
			info := top[0]
			if info.Transport != nil || info.Port != nil || info.URL != nil {
				t.Errorf("unbound entry has non-null transport/port/url: %+v", info)
			}
			if info.Note == "" || !strings.Contains(info.Note, tc.wantInNote) {
				t.Errorf("note = %q, want it to mention %q", info.Note, tc.wantInNote)
			}
		})
	}
}

// TestBoundServerHasNoNote pins that the note field is absent for a server
// that is actually bound (omitempty in the admin JSON).
func TestBoundServerHasNoNote(t *testing.T) {
	doc, err := engine.Parse([]byte(grpcDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	srv, err := Serve(doc, Options{AdminPortEphemeral: true, Ports: map[string]int{"main": freeTCPPort(t)}, BaseDir: "testdata"})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Wait()
	}()
	if info := srv.Topology()[0]; info.Note != "" {
		t.Errorf("bound server carries a note: %q", info.Note)
	}
}

// TestDescriptorPathConfined pins the §14 rule: a descriptorSet path may not
// escape the document directory. A document is portable, so what counts as
// "absolute" can't depend on the OS serving it — a POSIX path like
// "/etc/passwd" must be rejected on a Windows host too, and a Windows path
// like `C:\secret.binpb` must be rejected on a Linux/macOS host (regression:
// this used to rely on filepath.IsAbs, which only recognizes volume-rooted
// paths on Windows and silently let "/etc/passwd" through there).
func TestDescriptorPathConfined(t *testing.T) {
	refs := []string{
		"../secret.binpb",
		"/etc/passwd",
		`\etc\passwd`,
		`C:\Windows\System32\config`,
		`C:/Windows/System32/config`,
		`\\evil-host\share\x.binpb`,
		"https://evil/x.binpb",
	}
	for _, ref := range refs {
		if _, err := resolveDescriptorPath("testdata", ref); err == nil {
			t.Errorf("resolveDescriptorPath(%q) = nil error, want rejection", ref)
		}
	}
	if _, err := resolveDescriptorPath("testdata", "shop.binpb"); err != nil {
		t.Errorf("resolveDescriptorPath(shop.binpb) = %v, want ok", err)
	}
}

func TestGRPCCallsFacet(t *testing.T) {
	cc, schema := grpcFixture(t)
	md, _ := schema.method("shop.v1.ReportService", "GetReport")

	first, _, _, err := invokeUnary(t, cc, md, "shop.v1.ReportService", "GetReport", `{}`, nil)
	if err != nil || first["state"] != "GENERATING" {
		t.Fatalf("1st GetReport = %v, %v; want GENERATING", first, err)
	}
	second, _, _, err := invokeUnary(t, cc, md, "shop.v1.ReportService", "GetReport", `{}`, nil)
	if err != nil || second["state"] != "READY" {
		t.Fatalf("2nd GetReport = %v, %v; want READY", second, err)
	}
}

// --- client helpers ---

func invokeUnary(t *testing.T, cc *grpc.ClientConn, md protoreflect.MethodDescriptor, service, rpc, reqJSON string, callMD metadata.MD) (map[string]any, metadata.MD, metadata.MD, error) {
	t.Helper()
	ctx := context.Background()
	if callMD != nil {
		ctx = metadata.NewOutgoingContext(ctx, callMD)
	}
	req := encodeReq(t, md, reqJSON)
	var resp rawMessage
	var hdr, tlr metadata.MD
	err := cc.Invoke(ctx, "/"+service+"/"+rpc, &req, &resp, grpc.Header(&hdr), grpc.Trailer(&tlr))
	if err != nil {
		return nil, hdr, tlr, err
	}
	return decodeResp(t, md, resp), hdr, tlr, nil
}

func invokeStream(t *testing.T, cc *grpc.ClientConn, md protoreflect.MethodDescriptor, service, rpc, reqJSON string) ([]map[string]any, error) {
	t.Helper()
	ctx := context.Background()
	cs, err := cc.NewStream(ctx, &grpc.StreamDesc{StreamName: rpc, ServerStreams: true}, "/"+service+"/"+rpc)
	if err != nil {
		return nil, err
	}
	req := encodeReq(t, md, reqJSON)
	if err := cs.SendMsg(&req); err != nil {
		return nil, err
	}
	if err := cs.CloseSend(); err != nil {
		return nil, err
	}
	var out []map[string]any
	for {
		var raw rawMessage
		if err := cs.RecvMsg(&raw); err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, decodeResp(t, md, raw))
	}
}

func encodeReq(t *testing.T, md protoreflect.MethodDescriptor, reqJSON string) rawMessage {
	t.Helper()
	msg := dynamicpb.NewMessage(md.Input())
	if err := protojson.Unmarshal([]byte(reqJSON), msg); err != nil {
		t.Fatalf("encode request %q: %v", reqJSON, err)
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return rawMessage(b)
}

func decodeResp(t *testing.T, md protoreflect.MethodDescriptor, raw rawMessage) map[string]any {
	t.Helper()
	msg := dynamicpb.NewMessage(md.Output())
	if err := proto.Unmarshal(raw, msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	b, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal response json: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("unmarshal response json: %v", err)
	}
	return v
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
