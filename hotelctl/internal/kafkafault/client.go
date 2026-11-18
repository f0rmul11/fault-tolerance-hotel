package kafkafault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	address string
	http    *http.Client
}

func NewClient(address string) *Client {
	return &Client{
		address: address,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *Client) Close() {
	c.http.CloseIdleConnections()
}

func (c *Client) request(ctx context.Context, method, path string, body any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.address+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Toxiproxy %s %s: HTTP %d: %s", method, path, resp.StatusCode, response)
	}
	return nil
}

func (c *Client) BlockResponses(ctx context.Context) error {
	for node := 1; node <= 3; node++ {
		if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/proxies/broker-%d/toxics", node), map[string]any{
			"name":       "lesson7-response",
			"type":       "timeout",
			"stream":     "downstream",
			"toxicity":   1,
			"attributes": map[string]int{"timeout": 0},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Disconnect(ctx context.Context) error {
	for node := 1; node <= 3; node++ {
		if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/proxies/broker-%d", node), map[string]bool{
			"enabled": false,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Restore(ctx context.Context) error {
	var failures []error
	for node := 1; node <= 3; node++ {
		path := fmt.Sprintf("/proxies/broker-%d", node)
		if err := c.request(ctx, http.MethodDelete, path+"/toxics/lesson7-response", nil); err != nil {
			failures = append(failures, err)
		}
		if err := c.request(ctx, http.MethodPost, path, map[string]bool{"enabled": true}); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("не удалось восстановить Kafka proxy: %v", failures)
	}
	return nil
}
