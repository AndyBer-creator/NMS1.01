package grpcapi

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeClientConn struct {
	lastMethod string
	lastArgs   any
	out        any
	err        error
}

func (f *fakeClientConn) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	f.lastMethod = method
	f.lastArgs = args
	if f.out != nil {
		reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(f.out).Elem())
	}
	return f.err
}

func (f *fakeClientConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("not implemented")
}

type fakeRegistrar struct {
	desc    *grpc.ServiceDesc
	handler any
}

func (r *fakeRegistrar) RegisterService(desc *grpc.ServiceDesc, impl any) {
	r.desc = desc
	r.handler = impl
}

type fakeTrapHandler struct {
	lastReq *TrapIngestRequest
	resp    *TrapIngestResponse
	err     error
}

func (h *fakeTrapHandler) IngestTrap(ctx context.Context, req *TrapIngestRequest) (*TrapIngestResponse, error) {
	h.lastReq = req
	return h.resp, h.err
}

func TestJSONCodec_RoundTrip(t *testing.T) {
	t.Parallel()
	c := JSONCodec{}
	in := &TrapIngestRequest{
		DeviceIP: "10.0.0.1",
		OID:      ".1.3.6.1.2.1.1.3.0",
		Uptime:   42,
		TrapVars: map[string]string{"k": "v"},
	}
	b, err := c.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out TrapIngestRequest
	if err := c.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.DeviceIP != in.DeviceIP || out.OID != in.OID || out.Uptime != in.Uptime || out.TrapVars["k"] != "v" {
		t.Fatalf("unexpected round-trip: %+v", out)
	}
	if c.Name() != "json" {
		t.Fatalf("expected codec name json, got %q", c.Name())
	}
}

func TestTrapServiceClient_UsesMethodName(t *testing.T) {
	t.Parallel()
	cc := &fakeClientConn{out: &TrapIngestResponse{Status: "ok"}}
	c := NewTrapServiceClient(cc)
	resp, err := c.IngestTrap(context.Background(), &TrapIngestRequest{DeviceIP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("IngestTrap: %v", err)
	}
	if cc.lastMethod != TrapServiceIngestMethod {
		t.Fatalf("expected method %q, got %q", TrapServiceIngestMethod, cc.lastMethod)
	}
	if resp == nil || resp.Status != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRegisterTrapService_HandlerPaths(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistrar{}
	h := &fakeTrapHandler{resp: &TrapIngestResponse{Status: "ok"}}
	RegisterTrapService(reg, h)

	if reg.desc == nil || reg.handler == nil {
		t.Fatalf("expected service to be registered")
	}
	if reg.desc.ServiceName != "nms.v1.TrapService" {
		t.Fatalf("unexpected service name: %q", reg.desc.ServiceName)
	}
	if len(reg.desc.Methods) != 1 || reg.desc.Methods[0].MethodName != "IngestTrap" {
		t.Fatalf("unexpected methods: %+v", reg.desc.Methods)
	}

	methodHandler := reg.desc.Methods[0].Handler

	t.Run("decode error becomes InvalidArgument", func(t *testing.T) {
		_, err := methodHandler(h, context.Background(), func(any) error { return errors.New("bad decode") }, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("no interceptor calls handler", func(t *testing.T) {
		req := &TrapIngestRequest{DeviceIP: "10.0.0.9"}
		out, err := methodHandler(h, context.Background(), func(v any) error {
			*v.(*TrapIngestRequest) = *req
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		if h.lastReq == nil || h.lastReq.DeviceIP != "10.0.0.9" {
			t.Fatalf("unexpected handler req: %+v", h.lastReq)
		}
		if out.(*TrapIngestResponse).Status != "ok" {
			t.Fatalf("unexpected response: %+v", out)
		}
	})

	t.Run("interceptor path receives info and request", func(t *testing.T) {
		req := &TrapIngestRequest{DeviceIP: "10.0.0.10"}
		var gotMethod string
		interceptor := func(ctx context.Context, in any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			gotMethod = info.FullMethod
			return handler(ctx, in)
		}
		out, err := methodHandler(h, context.Background(), func(v any) error {
			*v.(*TrapIngestRequest) = *req
			return nil
		}, interceptor)
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if gotMethod != TrapServiceIngestMethod {
			t.Fatalf("expected FullMethod %q, got %q", TrapServiceIngestMethod, gotMethod)
		}
		if out.(*TrapIngestResponse).Status != "ok" {
			t.Fatalf("unexpected response: %+v", out)
		}
	})
}

