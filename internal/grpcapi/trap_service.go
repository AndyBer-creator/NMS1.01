package grpcapi

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TrapServiceIngestMethod is the fully-qualified unary RPC method name.
const TrapServiceIngestMethod = "/nms.v1.TrapService/IngestTrap"

// TrapIngestRequest contains trap payload fields sent over gRPC.
type TrapIngestRequest struct {
	DeviceIP string            `json:"device_ip"`
	OID      string            `json:"oid"`
	Uptime   int64             `json:"uptime"`
	TrapVars map[string]string `json:"trap_vars"`
}

// TrapIngestResponse returns ingest status for trap forwarding clients.
type TrapIngestResponse struct {
	Status string `json:"status"`
}

// TrapServiceHandler defines server-side trap ingest behavior.
type TrapServiceHandler interface {
	IngestTrap(ctx context.Context, req *TrapIngestRequest) (*TrapIngestResponse, error)
}

// TrapServiceClient defines client-side trap ingest calls.
type TrapServiceClient interface {
	IngestTrap(ctx context.Context, req *TrapIngestRequest, opts ...grpc.CallOption) (*TrapIngestResponse, error)
}

type trapServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewTrapServiceClient builds a typed client over a gRPC connection.
func NewTrapServiceClient(cc grpc.ClientConnInterface) TrapServiceClient {
	return &trapServiceClient{cc: cc}
}

func (c *trapServiceClient) IngestTrap(ctx context.Context, req *TrapIngestRequest, opts ...grpc.CallOption) (*TrapIngestResponse, error) {
	out := new(TrapIngestResponse)
	if err := c.cc.Invoke(ctx, TrapServiceIngestMethod, req, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterTrapService registers TrapService handler on a gRPC server.
func RegisterTrapService(s grpc.ServiceRegistrar, handler TrapServiceHandler) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "nms.v1.TrapService",
		HandlerType: (*TrapServiceHandler)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "IngestTrap",
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					in := new(TrapIngestRequest)
					if err := dec(in); err != nil {
						return nil, status.Error(codes.InvalidArgument, err.Error())
					}
					if interceptor == nil {
						return srv.(TrapServiceHandler).IngestTrap(ctx, in)
					}
					info := &grpc.UnaryServerInfo{
						Server:     srv,
						FullMethod: TrapServiceIngestMethod,
					}
					handlerFn := func(ctx context.Context, req interface{}) (interface{}, error) {
						return srv.(TrapServiceHandler).IngestTrap(ctx, req.(*TrapIngestRequest))
					}
					return interceptor(ctx, in, info, handlerFn)
				},
			},
		},
	}, handler)
}

// JSONCodec is a lightweight JSON codec used for pilot internal gRPC traffic.
type JSONCodec struct{}

// Name returns codec name used by gRPC content-subtype matching.
func (JSONCodec) Name() string { return "json" }

// Marshal encodes request/response messages to JSON bytes.
func (JSONCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON bytes into request/response message structs.
func (JSONCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
