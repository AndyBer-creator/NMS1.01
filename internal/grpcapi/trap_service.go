package grpcapi

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const TrapServiceIngestMethod = "/nms.v1.TrapService/IngestTrap"

type TrapIngestRequest struct {
	DeviceIP string            `json:"device_ip"`
	OID      string            `json:"oid"`
	Uptime   int64             `json:"uptime"`
	TrapVars map[string]string `json:"trap_vars"`
}

type TrapIngestResponse struct {
	Status string `json:"status"`
}

type TrapServiceHandler interface {
	IngestTrap(ctx context.Context, req *TrapIngestRequest) (*TrapIngestResponse, error)
}

type TrapServiceClient interface {
	IngestTrap(ctx context.Context, req *TrapIngestRequest, opts ...grpc.CallOption) (*TrapIngestResponse, error)
}

type trapServiceClient struct {
	cc grpc.ClientConnInterface
}

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

type JSONCodec struct{}

func (JSONCodec) Name() string { return "json" }

func (JSONCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (JSONCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
