package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRejectsTyposAndInvalidPorts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"unknown service field":  strings.Replace(string(raw), "- review", "- name: review\n    typo: true", 1),
		"duplicate service port": strings.Replace(string(raw), "- review", "- name: review\n    grpc: {host: 9090}", 1),
		"unknown field":          string(raw) + "agents: 2\n",
		"duplicate port":         strings.Replace(string(raw), "runtime:", "runtime:\n  ports: {grpc: 8080}", 1),
		"escaping source":        strings.Replace(string(raw), "- api-gateway", "- name: api-gateway\n    context: ../outside", 1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir, ""); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestDiscoverFromServiceDirectory(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(filepath.Join(root, "services", "api-gateway"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != root {
		t.Fatalf("root %s, expected %s", c.Root, root)
	}
}

func TestServiceDefaultsAndExplicitOverrides(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Services) != 6 || len(c.PortBindings()) != 20 {
		t.Fatalf("unexpected service/port count: %d/%d", len(c.Services), len(c.PortBindings()))
	}
	for i, service := range c.Services[1:] {
		if service.GRPC.Host != 9091+i || service.Admin.Host != 8082+i {
			t.Fatalf("wrong ports for %s", service.Name)
		}
		if service.Context != "services/"+service.Name ||
			service.Manifests != "services/"+service.Name+"/build/deployment" {
			t.Fatalf("wrong paths for %s", service.Name)
		}
	}
	dir := t.TempDir()
	raw := "services:\n  - name: booking\n    grpc: {host: 19092, node: 31092}\n    admin: {host: 18083, node: 31083}\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Services[0].GRPC.Host != 19092 || c.Services[0].Admin.Node != 31083 {
		t.Fatal("defaults overwrote explicit ports")
	}
}
