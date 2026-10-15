package rtconfig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// Connection describes the operator connection, independent of k3s and Docker.
type Connection struct {
	Endpoints   []string
	CAFile      string
	CertFile    string
	KeyFile     string
	Username    string
	PasswordEnv string
}

func (o Connection) Validate() error {
	if len(o.Endpoints) == 0 {
		return fmt.Errorf("at least one etcd endpoint is required")
	}
	var scheme string
	for _, endpoint := range o.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("invalid etcd endpoint URL")
		}
		if u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("etcd endpoint must be an http(s) URL with a host")
		}
		if scheme != "" && scheme != u.Scheme {
			return fmt.Errorf("etcd endpoints must use the same URL scheme")
		}
		scheme = u.Scheme
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("etcd endpoint must not contain credentials, query or path")
		}
		if u.Scheme != "https" && (o.CAFile != "" || o.CertFile != "" || o.KeyFile != "") {
			return fmt.Errorf("TLS files require https etcd endpoints")
		}
	}
	if (o.CertFile == "") != (o.KeyFile == "") {
		return fmt.Errorf("--cert and --key must be specified together")
	}
	if o.Username != "" && o.PasswordEnv == "" {
		return fmt.Errorf("--user requires --password-env")
	}
	return nil
}

// Open inherits the entire command deadline, including authentication in New.
func Open(ctx context.Context, o Connection) (*Client, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	transport, err := o.tlsConfig()
	if err != nil {
		return nil, err
	}
	var password string
	if o.Username != "" {
		var exists bool
		password, exists = os.LookupEnv(o.PasswordEnv)
		if !exists || password == "" {
			return nil, fmt.Errorf("etcd password environment variable %q is empty or missing", o.PasswordEnv)
		}
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:          o.Endpoints,
		TLS:                transport,
		Username:           o.Username,
		Password:           password,
		Context:            ctx,
		DialTimeout:        5 * time.Second,
		MaxCallRecvMsgSize: 1 << 20,
		MaxUnaryRetries:    1, // One attempt: never hide an uncertain write behind a retry.
		Logger:             zap.NewNop(),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to etcd: %w", err)
	}
	return &Client{client: client}, nil
}

func (o Connection) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.CAFile != "" {
		pem, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read etcd CA: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("etcd CA file contains no certificates")
		}
	}
	if o.CertFile != "" {
		certificate, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load etcd client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{certificate}
	}
	// A non-nil TLS configuration also enables TLS for http endpoints in the
	// SDK; leave it nil for the explicit local plaintext connection.
	for _, endpoint := range o.Endpoints {
		u, _ := url.Parse(endpoint) // Validate has already checked every URL.
		if u.Scheme == "https" {
			return cfg, nil
		}
	}
	return nil, nil
}
