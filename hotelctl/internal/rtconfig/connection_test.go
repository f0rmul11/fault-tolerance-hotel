package rtconfig

import (
	"context"
	"strings"
	"testing"
)

func TestConnectionValidation(t *testing.T) {
	for _, endpoints := range [][]string{
		nil,
		{"localhost:2379"},
		{"file:///tmp/etcd"},
		{"http://operator:secret@localhost:2379"},
		{"http://localhost:2379/private"},
		{"http://localhost:2379?password=secret"},
		{"http://localhost:2379", "https://localhost:2379"},
	} {
		if err := (Connection{Endpoints: endpoints}).Validate(); err == nil {
			t.Fatalf("invalid endpoints accepted: %v", endpoints)
		}
	}
	for _, options := range []Connection{
		{
			Endpoints: []string{"https://localhost:2379"},
			CertFile:  "client.pem",
		},
		{
			Endpoints: []string{"http://localhost:2379"},
			CAFile:    "ca.pem",
		},
	} {
		if err := options.Validate(); err == nil {
			t.Fatal("invalid TLS options accepted")
		}
	}
}

func TestPasswordComesFromEnvironment(t *testing.T) {
	const name = "HOTELCTL_MISSING_PASSWORD_TEST"
	t.Setenv(name, "")
	client, err := Open(context.Background(), Connection{
		Endpoints:   []string{"http://127.0.0.1:1"},
		Username:    "operator",
		PasswordEnv: name,
	})
	if client != nil || err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("missing password must fail before dialing: %v", err)
	}
}
