package worker

import (
	"context"
	"testing"
	"time"
)

func TestStopAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := NewWorker(
		ctx,
		func(context.Context) {},
		func(context.Context) time.Duration {
			return time.Millisecond
		},
		func(context.Context) int {
			return 1
		},
	)
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	stopped := make(chan error, 1)
	go func() {
		stopped <- worker.Stop()
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop blocked after cancellation")
	}
	if err := worker.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestStopWaitsForRunningTask(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	worker := NewWorker(
		context.Background(),
		func(context.Context) {
			close(started)
			<-release
		},
		func(context.Context) time.Duration {
			return time.Millisecond
		},
		func(context.Context) int {
			return 1
		},
	)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	stopped := make(chan error, 1)
	go func() {
		stopped <- worker.Stop()
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before task finished")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish")
	}
}
