package intercept

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestClientNameReplacesIdentityWithoutMutatingCaller(t *testing.T) {
	original := metadata.Pairs("x-client-name", "spoofed", "trace", "keep")
	ctx := metadata.NewOutgoingContext(t.Context(), original)
	err := ClientName("api-gateway")(ctx, "/test/Read", nil, nil, nil,
		func(actual context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(actual)
			if len(md.Get("x-client-name")) != 1 || md.Get("x-client-name")[0] != "api-gateway" || md.Get("trace")[0] != "keep" {
				t.Fatalf("outgoing metadata=%v", md)
			}
			return nil
		},
	)
	if err != nil || original.Get("x-client-name")[0] != "spoofed" {
		t.Fatalf("caller mutated: %v %v", original, err)
	}
}
