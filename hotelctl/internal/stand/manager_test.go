package stand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

type runtimeStub struct {
	Runtime
	ensureErr  error
	destroyErr error
	owners     []string
	status     project.RuntimeStatus
}

func (r *runtimeStub) Check(context.Context) error { return nil }
func (r *runtimeStub) Ensure(_ context.Context, state *project.State, _ io.Writer) error {
	r.owners = append(r.owners, state.Owner)
	return r.ensureErr
}
func (r *runtimeStub) Logs(context.Context, *project.State, io.Writer) error { return nil }
func (r *runtimeStub) Destroy(context.Context, *project.State) error         { return r.destroyErr }
func (r *runtimeStub) Status(context.Context, *project.State) (project.RuntimeStatus, error) {
	return r.status, nil
}

func newTestManager(t *testing.T, runtime *runtimeStub, out io.Writer) *Manager {
	t.Helper()
	c := config.Config{Root: t.TempDir(), Name: "test", Runtime: config.Runtime{Image: "rancher/k3s:test"}}
	return New(c, runtime, iostreams.IOStreams{Out: out, ErrOut: io.Discard})
}

func TestFailedStartupKeepsOwnerForRetry(t *testing.T) {
	failure := errors.New("simulated Docker start failure")
	runtime := &runtimeStub{ensureErr: failure}
	manager := newTestManager(t, runtime, io.Discard)
	for range 2 {
		if err := manager.Up(context.Background(), nil); !errors.Is(err, failure) {
			t.Fatalf("up: %v", err)
		}
	}
	state, err := manager.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || len(runtime.owners) != 2 || state.Owner == "" || runtime.owners[0] != state.Owner || runtime.owners[1] != state.Owner {
		t.Fatalf("retry changed ownership: state=%+v owners=%v", state, runtime.owners)
	}
}

func TestFailedDestroyPreservesStateForRetry(t *testing.T) {
	failure := errors.New("simulated volume removal failure")
	runtime := &runtimeStub{destroyErr: failure}
	manager := newTestManager(t, runtime, io.Discard)
	before, err := manager.load(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Destroy(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("destroy: %v", err)
	}
	after, err := manager.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || after.Owner != before.Owner {
		t.Fatal("failed deletion lost the state needed to retry cleanup")
	}
	runtime.destroyErr = nil
	if err := manager.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err = manager.Store.Load()
	if err != nil || after != nil {
		t.Fatalf("successful deletion left state: %v, %v", after, err)
	}
}

func TestJSONStatusReportsKubernetesFailureAsError(t *testing.T) {
	runtime := &runtimeStub{status: project.RuntimeStatus{Name: "test-k3s", Exists: true, Running: true}}
	var out bytes.Buffer
	manager := newTestManager(t, runtime, &out)
	if _, err := manager.load(true); err != nil {
		t.Fatal(err)
	}
	// The node exists, but its host kubeconfig is missing after a partial startup.
	if err := manager.Status(context.Background(), "json"); err == nil {
		t.Fatal("status must fail when Kubernetes cannot be read")
	}
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Fatal("JSON must preserve the diagnostic")
	}
}
