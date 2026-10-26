package fault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Runtime interface {
	Values(context.Context) (map[string]string, error)
	Modify(context.Context, map[string]string) error
}

// RuntimeClient uses Envoy's existing admin API; no custom control service.
type RuntimeClient struct {
	Address string
	HTTP    *http.Client
}

func NewRuntimeClient(address string) *RuntimeClient {
	return &RuntimeClient{
		Address: address,
		HTTP: &http.Client{
			Transport: &http.Transport{Proxy: nil},
			Timeout:   5 * time.Second,
		},
	}
}

func (c *RuntimeClient) Close() {
	c.HTTP.CloseIdleConnections()
}

func (c *RuntimeClient) request(ctx context.Context, method, path, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Address+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Envoy admin: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *RuntimeClient) Values(ctx context.Context) (map[string]string, error) {
	data, err := c.request(ctx, http.MethodGet, "/runtime", "")
	if err != nil {
		return nil, err
	}
	var result struct {
		Entries map[string]struct {
			FinalValue string `json:"final_value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	values := make(map[string]string, len(result.Entries))
	for key, value := range result.Entries {
		values[key] = value.FinalValue
	}
	return values, nil
}

func (c *RuntimeClient) Modify(ctx context.Context, values map[string]string) error {
	form := url.Values{}
	for key, value := range values {
		form.Set(key, value)
	}
	_, err := c.request(ctx, http.MethodPost, "/runtime_modify", form.Encode())
	return err
}

func (o Options) Values() map[string]string {
	prefix := "demo." + o.From + "." + o.To + "."
	values := map[string]string{
		prefix + "request.delay_percent":  "0",
		prefix + "request.delay_ms":       "1",
		prefix + "request.abort_percent":  "0",
		prefix + "response.delay_percent": "0",
		prefix + "response.delay_ms":      "1",
	}
	if o.Unavailable {
		values[prefix+"request.abort_percent"] = "100"
	} else if !o.Clear {
		values[prefix+o.Direction+".delay_percent"] = "100"
		values[prefix+o.Direction+".delay_ms"] = strconv.FormatInt(o.Slowdown.Milliseconds(), 10)
	}
	return values
}

func ClearRuntime(ctx context.Context, runtime Runtime, options Options) error {
	options.Clear, options.Unavailable = true, false
	values := options.Values()
	if err := runtime.Modify(ctx, values); err != nil {
		return err
	}
	return verifyRuntime(ctx, runtime, values)
}

func verifyRuntime(ctx context.Context, runtime Runtime, expected map[string]string) error {
	observed, err := runtime.Values(ctx)
	if err != nil {
		return err
	}
	for key, value := range expected {
		if observed[key] != value {
			return fmt.Errorf("Envoy не применил %s: ожидается %s, получено %q", key, value, observed[key])
		}
	}
	return nil
}

// RunRuntime keeps cleanup in the foreground CLI. SIGKILL requires --clear.
func RunRuntime(ctx context.Context, runtime Runtime, options Options, out io.Writer) (err error) {
	if err := options.Validate(); err != nil {
		return err
	}
	if options.Clear {
		return ClearRuntime(ctx, runtime, options)
	}
	current, err := runtime.Values(ctx)
	if err != nil {
		return err
	}
	values := options.Values()
	for key := range values {
		if strings.HasSuffix(key, "_percent") && current[key] != "" && current[key] != "0" {
			return fmt.Errorf("на %s → %s уже действует fault; сначала hotelctl fault %s %s --clear", options.From, options.To, options.From, options.To)
		}
	}
	// Register before Modify: its response can be lost after a successful apply.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if clearErr := ClearRuntime(cleanup, runtime, options); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("не удалось снять fault: %w; выполните hotelctl fault %s %s --clear", clearErr, options.From, options.To))
			return
		}
		fmt.Fprintln(out, "Правило снято; новые вызовы проходят без этой инъекции")
	}()
	if err := runtime.Modify(ctx, values); err != nil {
		return err
	}
	if err := verifyRuntime(ctx, runtime, values); err != nil {
		return err
	}
	if options.Unavailable {
		fmt.Fprintf(out, "ACTIVE %s → %s: UNAVAILABLE на %s\n", options.From, options.To, options.Duration)
	} else {
		fmt.Fprintf(out, "ACTIVE %s → %s: %s +%s на %s\n", options.From, options.To, options.Direction, options.Slowdown, options.Duration)
	}
	fmt.Fprintln(out, "Отправьте запрос из второго терминала. Ctrl+C досрочно снимает правило.")
	timer := time.NewTimer(options.Duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
