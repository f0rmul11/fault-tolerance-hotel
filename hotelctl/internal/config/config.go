package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

const FileName = "hotel.yaml"

type Ports struct {
	HTTP  int `json:"http"`
	GRPC  int `json:"grpc"`
	Admin int `json:"admin"`
	API   int `json:"api"`
}

type Runtime struct {
	Image       string `json:"image"`
	BindAddress string `json:"bindAddress"`
	Ports       Ports  `json:"ports"`
}

// PortBinding publishes a Kubernetes NodePort on the student's computer.
type PortBinding struct {
	Host      int  `json:"host"`
	Node      int  `json:"node"`
	LocalOnly bool `json:"localOnly,omitempty"`
}

type Service struct {
	Name       string      `json:"name"`
	Context    string      `json:"context"`
	Dockerfile string      `json:"dockerfile"`
	Manifests  string      `json:"manifests"`
	Image      string      `json:"image"`
	Namespace  string      `json:"namespace"`
	Deployment string      `json:"deployment"`
	GRPC       PortBinding `json:"grpc,omitempty"`
	Admin      PortBinding `json:"admin,omitempty"`
	EchoMethod string      `json:"echoMethod,omitempty"`
}

type Observability struct {
	Enabled bool   `json:"enabled"`
	Stage   string `json:"stage"`
}

type Config struct {
	APIVersion    string        `json:"apiVersion"`
	Name          string        `json:"name"`
	Runtime       Runtime       `json:"runtime"`
	Services      []Service     `json:"services"`
	Root          string        `json:"-"`
	Observability Observability `json:"observability,omitempty"`
}

// FindRoot walks up from a supplied directory; no dependency on a git executable.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found; run from the repository or use --project DIR", FileName)
		}
		dir = parent
	}
}

func Load(project, filename string) (Config, error) {
	if project == "" {
		project = "."
	}
	root, err := filepath.Abs(project)
	if err != nil {
		return Config{}, err
	}
	if filename == "" {
		root, err = FindRoot(root)
		if err != nil {
			return Config{}, err
		}
		filename = filepath.Join(root, FileName)
	} else if !filepath.IsAbs(filename) {
		filename = filepath.Join(root, filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	var c Config
	if err := yaml.UnmarshalStrict(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", filename, err)
	}
	c.Root = root
	c.applyDefaults()
	return c, c.Validate()
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

var methodPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*/[A-Za-z_][A-Za-z0-9_]*$`)

func (c Config) Validate() error {
	if c.APIVersion != "hotelflow/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", c.APIVersion)
	}
	if !namePattern.MatchString(c.Name) {
		return fmt.Errorf("invalid stand name %q", c.Name)
	}
	if c.Runtime.Image == "" || strings.HasSuffix(c.Runtime.Image, ":latest") {
		return fmt.Errorf("runtime.image must specify a pinned k3s image")
	}
	if c.Runtime.BindAddress != "127.0.0.1" && c.Runtime.BindAddress != "0.0.0.0" {
		return fmt.Errorf("runtime.bindAddress must be 127.0.0.1 or 0.0.0.0")
	}
	if c.Observability.Enabled && c.Observability.Stage != "traces" && c.Observability.Stage != "logs" && c.Observability.Stage != "metrics" {
		return fmt.Errorf("observability.stage: поддерживаются traces, logs и metrics")
	}
	if !c.Observability.Enabled && c.Observability.Stage != "" && c.Observability.Stage != "traces" && c.Observability.Stage != "logs" && c.Observability.Stage != "metrics" {
		return fmt.Errorf("observability.stage: неизвестный этап %q", c.Observability.Stage)
	}
	used := map[int]bool{}
	for _, p := range c.Runtime.Ports.All() {
		if p < 1024 || p > 65535 || used[p] {
			return fmt.Errorf("ports must be unique and in range 1024..65535 (got %d)", p)
		}
		used[p] = true
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("configure at least one service")
	}
	nodes := map[int]bool{
		6443:  true,
		30080: true,
		30090: true,
		30081: true,
	}
	names := map[string]bool{}
	for _, dependency := range Dependencies() {
		names[dependency.Name] = true
		if dependency.Port.Host == 0 {
			continue
		}
		if used[dependency.Port.Host] {
			return fmt.Errorf("порт %d занят локальным доступом к %s", dependency.Port.Host, dependency.Name)
		}
		used[dependency.Port.Host] = true
		nodes[dependency.Port.Node] = true
	}
	for _, component := range c.ObservabilityComponents() {
		names[component.Name] = true
	}
	for _, binding := range c.ObservabilityPortBindings() {
		if used[binding.Host] || nodes[binding.Node] {
			return fmt.Errorf("порт наблюдаемости %d или NodePort %d уже используется", binding.Host, binding.Node)
		}
		used[binding.Host], nodes[binding.Node] = true, true
	}
	for _, s := range c.Services {
		if !namePattern.MatchString(s.Name) || names[s.Name] {
			return fmt.Errorf("invalid or duplicate service %q", s.Name)
		}
		names[s.Name] = true
		for _, binding := range []PortBinding{
			s.GRPC,
			s.Admin,
		} {
			if binding == (PortBinding{}) {
				continue
			}
			if binding.Host < 1024 || binding.Host > 65535 || used[binding.Host] {
				return fmt.Errorf("service %s: host port %d is invalid or already used", s.Name, binding.Host)
			}
			if binding.Node < 30000 || binding.Node > 32767 || nodes[binding.Node] {
				return fmt.Errorf("service %s: NodePort %d is invalid or already used", s.Name, binding.Node)
			}
			used[binding.Host], nodes[binding.Node] = true, true
		}
		if s.EchoMethod != "" {
			if !methodPattern.MatchString(s.EchoMethod) {
				return fmt.Errorf("service %s: echoMethod must be package.Service/Method", s.Name)
			}
			if s.GRPC.Host == 0 && s.Name != "api-gateway" {
				return fmt.Errorf("service %s: echoMethod requires a published grpc port", s.Name)
			}
		}
		if s.Image == "" || !namePattern.MatchString(s.Namespace) || !namePattern.MatchString(s.Deployment) {
			return fmt.Errorf("service %s: image, namespace and deployment are required", s.Name)
		}
		for _, p := range []string{
			s.Context,
			s.Dockerfile,
			s.Manifests,
		} {
			clean := filepath.Clean(p)
			if p == "" || filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return fmt.Errorf("service %s: paths must stay within the project", s.Name)
			}
		}
	}
	return nil
}

func (p Ports) All() []int {
	return []int{
		p.HTTP,
		p.GRPC,
		p.Admin,
		p.API,
	}
}

func (c Config) Select(names []string) ([]Service, error) {
	if len(names) == 0 {
		return slices.Clone(c.Services), nil
	}
	result := make([]Service, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		i := slices.IndexFunc(c.Services, func(s Service) bool {
			return s.Name == name
		})
		if i < 0 {
			return nil, fmt.Errorf("unknown service %q; run hotelctl status to list services", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("service %q specified twice", name)
		}
		seen[name] = true
		result = append(result, c.Services[i])
	}
	return result, nil
}

func (c Config) StateDir() string {
	return filepath.Join(c.Root, ".local", "hotelctl")
}

func (c Config) Kubeconfig() string {
	return filepath.Join(c.StateDir(), "kubeconfig")
}

func (r Runtime) PortBindings() []PortBinding {
	return []PortBinding{
		{
			Host: r.Ports.API,
			Node: 6443,
		},
		{
			Host: r.Ports.HTTP,
			Node: 30080,
		},
		{
			Host: r.Ports.Admin,
			Node: 30081,
		},
		{
			Host: r.Ports.GRPC,
			Node: 30090,
		},
	}
}

func (c Config) PortBindings() []PortBinding {
	result := c.Runtime.PortBindings()
	result = append(result, c.ObservabilityPortBindings()...)
	for _, dependency := range Dependencies() {
		if dependency.Port.Host != 0 {
			result = append(result, dependency.Port)
		}
	}
	for _, service := range c.Services {
		for _, binding := range []PortBinding{
			service.GRPC,
			service.Admin,
		} {
			if binding != (PortBinding{}) {
				result = append(result, binding)
			}
		}
	}
	slices.SortFunc(result, func(a, b PortBinding) int {
		return a.Node - b.Node
	})
	return result
}

// The normal form is a service name. An object is only needed for overrides.
func (s *Service) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &s.Name)
	}
	type plain Service
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(s))
}

func (c *Config) applyDefaults() {
	if c.Observability.Enabled && c.Observability.Stage == "" {
		c.Observability.Stage = "traces"
	}
	if c.APIVersion == "" {
		c.APIVersion = "hotelflow/v1alpha1"
	}
	if c.Name == "" {
		c.Name = "hotelflow"
	}
	if c.Runtime.Image == "" {
		c.Runtime.Image = "rancher/k3s:v1.35.2-k3s1"
	}
	if c.Runtime.BindAddress == "" {
		c.Runtime.BindAddress = "127.0.0.1"
	}
	if c.Runtime.Ports.HTTP == 0 {
		c.Runtime.Ports.HTTP = 8080
	}
	if c.Runtime.Ports.GRPC == 0 {
		c.Runtime.Ports.GRPC = 9090
	}
	if c.Runtime.Ports.Admin == 0 {
		c.Runtime.Ports.Admin = 8081
	}
	if c.Runtime.Ports.API == 0 {
		c.Runtime.Ports.API = 6550
	}
	index := 0
	for i := range c.Services {
		s := &c.Services[i]
		if s.Context == "" {
			s.Context = "services/" + s.Name
		}
		if s.Dockerfile == "" {
			s.Dockerfile = "build/Dockerfile"
		}
		if s.Manifests == "" {
			s.Manifests = "services/" + s.Name + "/build/deployment"
		}
		if s.Image == "" {
			s.Image = "hotelflow/" + s.Name
		}
		if s.Namespace == "" {
			s.Namespace = "hotelflow"
		}
		if s.Deployment == "" {
			s.Deployment = s.Name
		}
		if s.EchoMethod == "" {
			s.EchoMethod = strings.ReplaceAll(s.Name, "-", "_") + ".echo.v1.EchoService/Echo"
		}
		if s.Name == "api-gateway" {
			continue
		}
		index++
		if s.GRPC.Host == 0 {
			s.GRPC.Host = c.Runtime.Ports.GRPC + index
		}
		if s.GRPC.Node == 0 {
			s.GRPC.Node = 30090 + index
		}
		if s.Admin.Host == 0 {
			s.Admin.Host = c.Runtime.Ports.Admin + index
		}
		if s.Admin.Node == 0 {
			s.Admin.Node = 30081 + index
		}
	}
}
