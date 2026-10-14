package stand

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

type resetStub struct {
	counts       map[string]int32
	resetErr     error
	restoreErr   error
	resetCalls   int
	cleaned      bool
	lastRestored string
}

func (s *resetStub) Replicas(_ context.Context, services []config.Service) ([]project.ServiceReplicas, error) {
	var result []project.ServiceReplicas
	for _, service := range services {
		result = append(result, project.ServiceReplicas{
			Service:  service,
			Replicas: s.counts[service.Name],
		})
	}
	return result, nil
}

func (s *resetStub) Scale(_ context.Context, service config.Service, replicas int32) error {
	if replicas > 0 && s.restoreErr != nil {
		return s.restoreErr
	}
	s.counts[service.Name] = replicas
	if replicas > 0 {
		s.lastRestored = service.Name
	}
	return nil
}

func (s *resetStub) WaitStopped(_ context.Context, service config.Service) error {
	if s.counts[service.Name] != 0 {
		return errors.New("приложение ещё работает")
	}
	return nil
}

func (s *resetStub) WaitDeployment(context.Context, config.Service) error {
	return nil
}

func (s *resetStub) ResetData(context.Context, *corev1.ConfigMap, *batchv1.Job, io.Writer) error {
	for _, count := range s.counts {
		if count != 0 {
			return errors.New("сброс начался до остановки приложений")
		}
	}
	s.resetCalls++
	return s.resetErr
}

func (s *resetStub) CleanupReset(context.Context, string) error {
	s.cleaned = true
	return nil
}

func TestResetResumesWithoutLosingReplicaCounts(t *testing.T) {
	for _, phase := range []string{"data", "restore"} {
		t.Run(phase, func(t *testing.T) {
			root, err := filepath.Abs("../../..")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(root, "")
			if err != nil {
				t.Fatal(err)
			}
			manager := New(cfg, nil, iostreams.IOStreams{Out: io.Discard})
			manager.Store.Dir = t.TempDir()
			state, err := manager.load(true)
			if err != nil {
				t.Fatal(err)
			}
			cluster := &resetStub{
				counts: map[string]int32{
					"api-gateway":  1,
					"booking":      2,
					"check-in":     1,
					"hotels":       1,
					"review":       1,
					"notification": 0,
				},
			}
			failure := errors.New("прервано")
			if phase == "data" {
				cluster.resetErr = failure
			} else {
				cluster.restoreErr = failure
			}
			if err := manager.reset(t.Context(), state, cluster); !errors.Is(err, failure) {
				t.Fatalf("ошибка сброса: %v", err)
			}
			for name, count := range cluster.counts {
				if count != 0 {
					t.Fatalf("%s запущен после незавершённого сброса", name)
				}
			}
			state, err = manager.Store.Load()
			if err != nil || state.Reset == nil || state.Reset.DataReset != (phase == "restore") {
				t.Fatalf("потеряна стадия сброса: %+v, %v", state, err)
			}
			cluster.resetErr, cluster.restoreErr = nil, nil
			if err := manager.reset(t.Context(), state, cluster); err != nil {
				t.Fatal(err)
			}
			wantCalls := 2
			if phase == "restore" {
				wantCalls = 1
			}
			if cluster.resetCalls != wantCalls || cluster.counts["booking"] != 2 || cluster.counts["notification"] != 0 || !cluster.cleaned {
				t.Fatalf("неверное восстановление: %+v", cluster)
			}
			if cluster.lastRestored != "api-gateway" {
				t.Fatalf("gateway должен запускаться последним: %s", cluster.lastRestored)
			}
			state, err = manager.Store.Load()
			if err != nil || state.Reset != nil {
				t.Fatalf("сброс остался незавершённым: %+v, %v", state, err)
			}
		})
	}
}

func TestUpDoesNotResumeInterruptedReset(t *testing.T) {
	runtime := &runtimeStub{}
	manager := newTestManager(t, runtime, io.Discard)
	state, err := manager.load(true)
	if err != nil {
		t.Fatal(err)
	}
	state.Reset = &project.ResetState{JobName: "interrupted-reset"}
	if err := manager.Store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := manager.Up(t.Context(), nil); err == nil || len(runtime.owners) != 0 {
		t.Fatalf("up начал запуск после незавершённого сброса: %v", err)
	}
}
