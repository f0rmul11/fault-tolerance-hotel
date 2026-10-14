package project

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/gofrs/flock"
)

type RuntimeStatus struct {
	Name    string `json:"name"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
}

type ServiceReplicas struct {
	Service  config.Service `json:"service"`
	Replicas int32          `json:"replicas"`
}

type ResetState struct {
	JobName   string            `json:"jobName"`
	Services  []ServiceReplicas `json:"services"`
	DataReset bool              `json:"dataReset"`
}

type State struct {
	Reset    *ResetState          `json:"reset,omitempty"`
	Version  int                  `json:"version"`
	Owner    string               `json:"owner"`
	Name     string               `json:"name"`
	Runtime  config.Runtime       `json:"runtime"`
	Images   map[string]string    `json:"images"`
	Bindings []config.PortBinding `json:"bindings,omitempty"`
}

type Store struct {
	Dir string
}

func (s Store) Lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(s.Dir, "operation.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("another hotelctl operation is running for this project")
	}
	return func() {
		_ = lock.Unlock()
	}, nil
}

func (s Store) Load() (*State, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("read stand state: %w", err)
	}
	if state.Version != 1 || state.Owner == "" || state.Name == "" {
		return nil, fmt.Errorf("invalid stand state; refusing to operate on unowned resources")
	}
	if state.Images == nil {
		state.Images = map[string]string{}
	}
	return &state, nil
}

func NewState(c config.Config) (*State, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &State{
		Version:  1,
		Owner:    hex.EncodeToString(id),
		Name:     c.Name,
		Runtime:  c.Runtime,
		Images:   map[string]string{},
		Bindings: c.PortBindings(),
	}, nil
}

func (s Store) Save(state *State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(s.Dir, "state.json"), append(data, '\n'))
}

func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s Store) Clear() error {
	for _, name := range []string{
		"state.json",
		"kubeconfig",
	} {
		if err := os.Remove(filepath.Join(s.Dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// PortBindings also reads state written before service port publication existed.
func (s *State) PortBindings() []config.PortBinding {
	if len(s.Bindings) == 0 {
		return s.Runtime.PortBindings()
	}
	return s.Bindings
}
