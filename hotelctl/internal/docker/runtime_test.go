package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

func TestDestroyValidatesVolumeOwnershipBeforeDeletingContainer(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "DELETE" {
			deletes.Add(1)
			w.WriteHeader(204)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/containers/"):
			fmt.Fprint(w, `{"Id":"container","Config":{"Labels":{"io.hotelflow.owner":"ours","io.hotelflow.managed-by":"hotelctl"}},"State":{"Running":true}}`)
		case strings.Contains(r.URL.Path, "/volumes/"):
			fmt.Fprint(w, `{"Name":"test-k3s-data","Labels":{"io.hotelflow.owner":"someone-else","io.hotelflow.managed-by":"hotelctl"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	api, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion("1.51"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{
		client: api,
	}
	defer engine.Close()
	err = engine.Destroy(context.Background(), &project.State{
		Name:  "test",
		Owner: "ours",
	})
	if err == nil || !strings.Contains(err.Error(), "another project") {
		t.Fatalf("got %v", err)
	}
	if deletes.Load() != 0 {
		t.Fatal("deleted resources before validating all ownership labels")
	}
}

func TestServicePortUpdatePreservesVolumeAndOwner(t *testing.T) {
	for _, owned := range []bool{
		true,
		false,
	} {
		t.Run(fmt.Sprint("owned=", owned), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			newHostPort := listener.Addr().(*net.TCPAddr).Port
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			state := &project.State{
				Name:  "test",
				Owner: "ours",
				Runtime: config.Runtime{
					Image:       "rancher/k3s:test",
					BindAddress: "127.0.0.1",
					Ports: config.Ports{
						API:   6550,
						HTTP:  8080,
						GRPC:  9090,
						Admin: 8081,
					},
				},
			}
			oldPorts := publishedPorts(state)
			state.Bindings = append(state.Runtime.PortBindings(), config.PortBinding{
				Host: newHostPort,
				Node: 30091,
			})
			var removed, created atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				path := strings.TrimPrefix(r.URL.Path, "/v1.51")
				switch {
				case r.Method == "GET" && path == "/containers/test-k3s/json":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"Id": "old",
						"Config": map[string]any{
							"Image":  state.Runtime.Image,
							"Labels": labels(state),
						},
						"State": map[string]any{
							"Running": false,
						},
						"HostConfig": map[string]any{
							"PortBindings": oldPorts,
						},
					})
				case r.Method == "GET" && path == "/volumes/test-k3s-data":
					owner := "ours"
					if !owned {
						owner = "someone-else"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"Name": "test-k3s-data",
						"Labels": map[string]string{
							ownerLabel:   owner,
							managedLabel: "hotelctl",
						},
					})
				case r.Method == "GET" && path == "/networks/test-network":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"Name":   "test-network",
						"Labels": labels(state),
					})
				case r.Method == "GET" && strings.HasPrefix(path, "/images/"):
					fmt.Fprint(w, `{"Id":"image"}`)
				case r.Method == "DELETE" && path == "/containers/old":
					if r.URL.Query().Get("v") == "1" {
						t.Error("must not delete volumes")
					}
					removed.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case r.Method == "POST" && path == "/containers/create":
					var body struct {
						Labels     map[string]string
						HostConfig struct {
							PortBindings nat.PortMap
							Mounts       []struct {
								Source string
								Target string
							}
						}
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Labels[ownerLabel] != state.Owner {
						t.Error("owner changed")
					}
					binding := body.HostConfig.PortBindings[nat.Port("30091/tcp")]
					if len(binding) != 1 || binding[0].HostPort != strconv.Itoa(newHostPort) {
						t.Error("new service port missing")
					}
					if len(body.HostConfig.Mounts) != 1 || body.HostConfig.Mounts[0].Source != "test-k3s-data" {
						t.Error("existing data volume not reused")
					}
					created.Add(1)
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, `{"Id":"new"}`)
				case r.Method == "POST" && path == "/containers/new/start":
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker operation: %s %s", r.Method, path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			api, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion("1.51"))
			if err != nil {
				t.Fatal(err)
			}
			engine := &Engine{
				client: api,
			}
			defer engine.Close()
			err = engine.Ensure(context.Background(), state, io.Discard)
			if owned {
				if err != nil {
					t.Fatal(err)
				}
				if removed.Load() != 1 || created.Load() != 1 {
					t.Fatal("container ports were not reconciled")
				}
			} else {
				if err == nil || removed.Load() != 0 || created.Load() != 0 {
					t.Fatalf("changed resources with foreign volume: %v", err)
				}
			}
		})
	}
}

func TestDependencyPortsStayOnLoopback(t *testing.T) {
	state := &project.State{
		Runtime: config.Runtime{
			BindAddress: "0.0.0.0",
		},
	}
	for _, dependency := range config.Dependencies() {
		if dependency.Port.Host != 0 {
			state.Bindings = append(state.Bindings, dependency.Port)
		}
	}
	for port, mappings := range publishedPorts(state) {
		for _, mapping := range mappings {
			if mapping.HostIP != "127.0.0.1" {
				t.Fatalf("dependency %s exposed on %s", port, mapping.HostIP)
			}
		}
	}
}

func TestNewPortPreflightRejectsOccupiedPortAndAcceptsOwnedBinding(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	desired := nat.PortMap{
		nat.Port("30686/tcp"): []nat.PortBinding{
			{
				HostIP:   "127.0.0.1",
				HostPort: port,
			},
		},
	}
	if err := checkNewPortBindings(nil, desired); err == nil {
		t.Fatal("occupied new UI port must reject runtime update")
	}
	if err := checkNewPortBindings(desired, desired); err != nil {
		t.Fatalf("existing owned mapping cannot be checked as a new listener: %v", err)
	}
}
