package hedge

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestSelectedPeerAndLogicalOnFinish(t *testing.T) {
	for _, code := range []codes.Code{codes.OK, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			h := NewHedger(Config{
				Default: DefaultConfig{
					MainConfig: MainConfig{
						Enabled:     true,
						WindowSize:  1,
						HedgeBudget: 0.5,
					},
				},
			})
			const method = "/test.Service/Get"
			state, _ := h.getByMethod(method)
			state.strategy.(*StrategySingleD).latencyRingBuf.Append(time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release := make(chan struct{})
			finished := make(chan struct{})
			defer func() {
				close(release)
				select {
				case <-finished:
				case <-ctx.Done():
					t.Error("late attempt did not finish")
				}
			}()

			var attempts, callbacks atomic.Int32
			var remote, pointerRemote peer.Peer
			reply := wrapperspb.String("old")
			check := func() {
				t.Helper()
				for _, p := range []peer.Peer{remote, pointerRemote} {
					if p.Addr == nil || p.Addr.String() != ":9090" {
						t.Errorf("unexpected selected peer: %v", p)
					}
				}
				if code == codes.OK && reply.Value != "selected" {
					t.Errorf("reply = %q, want selected", reply.Value)
				}
			}
			callback := func(err error) {
				callbacks.Add(1)
				if status.Code(err) != code {
					t.Errorf("callback code = %v, want %v", status.Code(err), code)
				}
				check()
			}
			invoke := func(_ context.Context, _ string, _, response any, _ *grpc.ClientConn, options ...grpc.CallOption) error {
				port := 9090
				if attempts.Add(1) == 1 {
					<-release
					defer close(finished)
					port = 9091
				}
				for _, option := range options {
					switch o := option.(type) {
					case grpc.PeerCallOption:
						*o.PeerAddr = peer.Peer{Addr: &net.TCPAddr{Port: port}}
					case *grpc.PeerCallOption:
						*o.PeerAddr = peer.Peer{Addr: &net.TCPAddr{Port: port}}
					case grpc.OnFinishCallOption, *grpc.OnFinishCallOption:
						t.Error("OnFinish leaked into an attempt")
					}
				}
				response.(*wrapperspb.StringValue).Value = "selected"
				return status.Error(code, "result")
			}
			err := h.UnaryClientInterceptor()(ctx, method, wrapperspb.String("request"), reply, nil, invoke,
				grpc.Peer(&remote), &grpc.PeerCallOption{PeerAddr: &pointerRemote},
				grpc.OnFinish(callback), &grpc.OnFinishCallOption{OnFinish: callback},
			)
			if status.Code(err) != code {
				t.Fatalf("code = %v, want %v", status.Code(err), code)
			}
			check()
			t.Cleanup(func() {
				check()
				if callbacks.Load() != 2 {
					t.Errorf("callback calls = %d, want one per registered callback", callbacks.Load())
				}
			})
		})
	}
}

func TestOnFinishWithoutSelectedAttempt(t *testing.T) {
	h := NewHedger(Config{
		Default: DefaultConfig{
			MainConfig: MainConfig{Enabled: true},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release := make(chan struct{})
	finished := make(chan struct{})
	defer func() {
		close(release)
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("attempt did not finish")
		}
	}()
	var calls atomic.Int32
	err := h.UnaryClientInterceptor()(ctx, "/test.Service/Get", wrapperspb.String(""), wrapperspb.String(""), nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			<-release
			defer close(finished)
			return context.Canceled
		},
		grpc.OnFinish(func(err error) {
			calls.Add(1)
			if status.Code(err) != codes.Canceled {
				t.Errorf("callback error = %v, want Canceled", err)
			}
		}),
	)
	if status.Code(err) != codes.Canceled || calls.Load() != 1 {
		t.Fatalf("error=%v, callbacks=%d", err, calls.Load())
	}
}

func TestConcurrentAttemptErrorsAreLocal(t *testing.T) {
	h := NewHedger(Config{
		Default: DefaultConfig{
			MainConfig: MainConfig{
				Enabled:     true,
				WindowSize:  1,
				HedgeBudget: 0.5,
			},
		},
	})
	const method = "/test.Service/Get"
	state, _ := h.getByMethod(method)
	state.strategy.(*StrategySingleD).latencyRingBuf.Append(time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	var calls atomic.Int32
	invoke := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		if calls.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := h.UnaryClientInterceptor()(ctx, method, wrapperspb.String(""), wrapperspb.String(""), nil, invoke); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("both attempts must run")
	}
}
