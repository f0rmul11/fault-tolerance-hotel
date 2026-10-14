package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
)

func gatewayHTTP(ctx context.Context, ports config.Ports, out io.Writer) error {
	transport := &http.Transport{
		Proxy: nil,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	base := "http://127.0.0.1:" + strconv.Itoa(ports.HTTP)
	message := "Привет, HotelFlow!"
	body, _ := json.Marshal(map[string]string{
		"message": message,
	})
	requests := []struct {
		method string
		url    string
		body   []byte
		echo   bool
	}{
		{
			"POST",
			base + "/v1/echo",
			body,
			true,
		},
		{
			"GET",
			base + "/v1/echo?message=" + url.QueryEscape(message),
			nil,
			true,
		},
		{
			"GET",
			base + "/swagger.json",
			nil,
			false,
		},
	}
	for _, probe := range []string{
		"live",
		"ready",
		"startup",
	} {
		requests = append(requests, struct {
			method string
			url    string
			body   []byte
			echo   bool
		}{
			"GET",
			"http://127.0.0.1:" + strconv.Itoa(ports.Admin) + "/healthcheck/" + probe,
			nil,
			false,
		})
	}
	for _, r := range requests {
		if err := retry(ctx, func() error {
			req, err := http.NewRequestWithContext(ctx, r.method, r.url, bytes.NewReader(r.body))
			if err != nil {
				return err
			}
			if r.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			response, err := client.Do(req)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("%s returned HTTP %d", r.url, response.StatusCode)
			}
			data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			if err != nil {
				return err
			}
			if r.echo {
				var got struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal(data, &got); err != nil {
					return err
				}
				if got.Message != message {
					return fmt.Errorf("unexpected Echo response: %q", got.Message)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "OK %s %s\n", r.method, r.url)
	}

	return nil
}

func retry(ctx context.Context, fn func() error) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		err := fn()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("smoke check: %w (%v)", ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

// Smoke checks only the selected services so up SERVICE is usable on its own.
func Smoke(ctx context.Context, cfg config.Config, services []config.Service, out io.Writer) error {
	transport := &http.Transport{
		Proxy: nil,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	for _, service := range services {
		port := service.GRPC.Host
		method := service.EchoMethod
		if service.Name == "api-gateway" {
			if err := gatewayHTTP(ctx, cfg.Runtime.Ports, out); err != nil {
				return err
			}
			port = cfg.Runtime.Ports.GRPC
			if method == "" {
				method = "api_gateway.echo.v1.EchoService/Echo"
			}
		}
		if method != "" {
			if err := retry(ctx, func() error {
				attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return echoRPC(attempt, "127.0.0.1:"+strconv.Itoa(port), method, service.Name)
			}); err != nil {
				return fmt.Errorf("%s gRPC Echo: %w", service.Name, err)
			}
			fmt.Fprintf(out, "OK %s gRPC Echo 127.0.0.1:%d\n", service.Name, port)
		}
		if service.Admin.Host == 0 {
			continue
		}
		for _, probe := range []string{
			"live",
			"ready",
			"startup",
		} {
			address := "http://127.0.0.1:" + strconv.Itoa(service.Admin.Host) + "/healthcheck/" + probe
			if err := retry(ctx, func() error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
				if err != nil {
					return err
				}
				response, err := client.Do(req)
				if err != nil {
					return err
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK {
					return fmt.Errorf("%s returned HTTP %d", address, response.StatusCode)
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(out, "OK %s %s\n", service.Name, probe)
		}
	}
	return nil
}
