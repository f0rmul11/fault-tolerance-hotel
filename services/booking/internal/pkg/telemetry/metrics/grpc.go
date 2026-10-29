package metrics

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type rpcMethodKey struct{}
type rpcStartKey struct{}
type serverHandler struct{}

// ServerHandler observes final gRPC statuses after error/auth interceptors,
// including framework errors and streaming RPCs. It composes with tracing.
func ServerHandler() stats.Handler { return serverHandler{} }

// TapHandler marks arrival at the transport before the per-RPC goroutine starts.
// Keep this hook cheap: it runs in the connection I/O goroutine. It never rejects
// requests. Admission-control rejections in another tap need separate accounting.
func TapHandler(ctx context.Context, _ *tap.Info) (context.Context, error) {
	return context.WithValue(ctx, rpcStartKey{}, time.Now()), nil
}

func (serverHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	method := info.FullMethodName
	if strings.HasPrefix(method, "/grpc.health.v1.Health/") || strings.HasPrefix(method, "/grpc.reflection.") {
		return ctx
	}
	method = knownMethod(method)
	// Export zeroes before an error first occurs, so rate has a baseline.
	for code := codes.OK; code <= codes.Unauthenticated; code++ {
		serverRequests.WithLabelValues("grpc", method, grpcCategory(code), code.String())
	}
	return context.WithValue(ctx, rpcMethodKey{}, method)
}

func (serverHandler) HandleRPC(ctx context.Context, event stats.RPCStats) {
	end, ok := event.(*stats.End)
	if !ok {
		return
	}
	method, ok := ctx.Value(rpcMethodKey{}).(string)
	if !ok {
		return
	}
	code := status.Code(end.Error)
	// Custom gRPC statuses outside the protocol's enum must not create labels.
	if code > codes.Unauthenticated {
		code = codes.Unknown
	}
	serverRequests.WithLabelValues("grpc", method, grpcCategory(code), code.String()).Inc()
	started, ok := ctx.Value(rpcStartKey{}).(time.Time)
	if !ok {
		started = end.BeginTime
	}
	serverDuration.WithLabelValues("grpc", method, grpcCategory(code)).Observe(end.EndTime.Sub(started).Seconds())
}

func (serverHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (serverHandler) HandleConn(context.Context, stats.ConnStats)                       {}

// Only names from compiled protobuf descriptors are labels. Arbitrary paths
// sent by clients share one series instead of creating unbounded cardinality.
func knownMethod(method string) string {
	service, name, ok := strings.Cut(strings.TrimPrefix(method, "/"), "/")
	if !ok {
		return "unknown"
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return "unknown"
	}
	sd, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok || sd.Methods().ByName(protoreflect.Name(name)) == nil {
		return "unknown"
	}
	return "/" + service + "/" + name
}
