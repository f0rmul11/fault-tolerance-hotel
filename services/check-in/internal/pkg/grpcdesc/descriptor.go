package grpcdesc

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"
)

// Descriptor connects generated gRPC registration to Clay without HTTP RPCs.
// Clay v0.3 requires RegisterHTTP and SwaggerDef even for a gRPC-only service.
type Descriptor struct {
	register func(*grpc.Server)
}

var _ transport.ServiceDesc = (*Descriptor)(nil)

func New(register func(*grpc.Server)) *Descriptor {
	return &Descriptor{
		register: register,
	}
}

func (d *Descriptor) RegisterGRPC(server *grpc.Server) {
	d.register(server)
}

func (d *Descriptor) RegisterHTTP(context.Context, *runtime.ServeMux) error {
	return nil
}

func (d *Descriptor) SwaggerDef() []byte {
	return []byte(`{"swagger":"2.0","info":{"title":"gRPC service","version":"1.0"},"paths":{}}`)
}
