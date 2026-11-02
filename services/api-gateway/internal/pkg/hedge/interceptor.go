package hedge

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// UnaryClientInterceptor унарный клиентский перехватчик
func (b *Hedger) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		state, ok := b.getByMethod(method)
		if !ok || state == nil {
			slog.Debug("no hedger found for method or disabled", "method", method)
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		message, ok := reply.(proto.Message)
		if !ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		messageType := message.ProtoReflect().Type()

		options := newCallOptions(opts)
		defer func() {
			options.finish(err)
		}()

		rpcFunc := func(ctx context.Context) (interface{}, error) {
			response := &attemptResult{reply: messageType.New().Interface()}
			attemptOptions := options.forAttempt(response)

			callErr := invoker(ctx, method, req, response.reply, cc, attemptOptions...)
			return response, callErr
		}

		winner, err := state.strategy.Execute(ctx, rpcFunc)
		if winner == nil {
			return err
		}
		response := winner.(*attemptResult)

		// Trailers могут содержать описание ошибки, поэтому публикуем их до проверки err.
		options.copyOutput(response)
		if err != nil {
			return err
		}

		proto.Reset(message)
		proto.Merge(message, response.reply)

		return nil
	}
}
