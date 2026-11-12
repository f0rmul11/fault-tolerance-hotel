package loadshedding

import (
	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func permanent(err error) bool {
	code := status.Code(err)
	return lo.Contains([]codes.Code{
		codes.ResourceExhausted, codes.Unimplemented, codes.Unauthenticated,
		codes.PermissionDenied, codes.InvalidArgument, codes.NotFound, codes.OutOfRange,
	}, code)
}

func temporary(err error) bool {
	code := status.Code(err)
	return lo.Contains([]codes.Code{
		codes.Internal, codes.Unknown, codes.Unavailable,
		codes.Canceled, codes.DeadlineExceeded,
	}, code)
}
