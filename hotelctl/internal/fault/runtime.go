package fault

import (
	"bufio"
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
		prefix + "request.delay_percent":         "0",
		prefix + "request.delay_ms":              "1",
		prefix + "request.abort_percent":         "0",
		prefix + "request.pushback_ms":           "disabled",
		prefix + "response.delay_percent":        "0",
		prefix + "response.delay_ms":             "1",
		prefix + "handler.delay_percent":         "0",
		prefix + "handler.abort_percent":         "0",
		prefix + "handler.delay_ms":              "1",
		prefix + "handler.delay_per_inflight_ms": "0",
		prefix + "handler.max_delay_ms":          "0",
		prefix + "handler.inflight_threshold":    "0",
		prefix + "handler.delay_ramp_ms":         "0",
		prefix + "handler.delay_ramp_start_ms":   "0",
	}
	values[prefix+"request.profile_percent"] = "0"
	values[prefix+"request.delay_profile"] = "disabled"
	if o.Clear {
		return values
	}
	if len(o.DelayProfile) != 0 {
		parts := make([]string, 0, len(o.DelayProfile))
		for _, step := range o.DelayProfile {
			parts = append(parts, fmt.Sprintf("%d:%d", step.Percent, step.Delay.Milliseconds()))
		}
		values[prefix+"request.delay_profile"] = strings.Join(parts, ",")
		values[prefix+"request.profile_percent"] = "100"
	} else if o.Unavailable {
		values[prefix+o.Direction+".abort_percent"] = strconv.Itoa(o.Percentage())
		if o.PushbackMS != nil {
			values[prefix+"request.pushback_ms"] = strconv.FormatInt(int64(*o.PushbackMS), 10)
		}
	} else if !o.Clear {
		values[prefix+o.Direction+".delay_percent"] = strconv.Itoa(o.Percentage())
		values[prefix+o.Direction+".delay_ms"] = strconv.FormatInt(o.Slowdown.Milliseconds(), 10)
		if o.DelayPerInflight > 0 {
			values[prefix+"handler.delay_per_inflight_ms"] = strconv.FormatInt(o.DelayPerInflight.Milliseconds(), 10)
			values[prefix+"handler.max_delay_ms"] = strconv.FormatInt(o.MaxDelay.Milliseconds(), 10)
			values[prefix+"handler.inflight_threshold"] = strconv.Itoa(o.InflightThreshold)
			values[prefix+"handler.delay_ramp_ms"] = strconv.FormatInt(o.DelayRamp.Milliseconds(), 10)
		}
	}
	return values
}

func ClearRuntime(ctx context.Context, runtime Runtime, options Options) error {
	options.Clear, options.Unavailable = true, false
	options.PushbackMS = nil
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
func RunRuntime(ctx context.Context, runtime Runtime, options Options, in io.Reader, out io.Writer) (err error) {
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
	if options.PushbackMS != nil && current["demo.pushback_supported"] != "1" {
		return fmt.Errorf("инфраструктура стенда ещё не поддерживает --pushback-ms; обновите её командой hotelctl deploy api-gateway")
	}
	if options.Direction == "handler" && current["demo.handler_delay_supported"] != "1" {
		return fmt.Errorf("обновите Review и Envoy: hotelctl build review && hotelctl deploy review")
	}
	if options.DelayPerInflight > 0 && current["demo.handler_inflight_delay_supported"] != "1" {
		return fmt.Errorf("обновите Review и Envoy для --delay-per-inflight: hotelctl build review && hotelctl deploy review")
	}
	if options.controlledInflight() && current["demo.handler_inflight_ramp_supported"] != "1" {
		return fmt.Errorf("обновите Review и Envoy для управляемой перегрузки: hotelctl build review && hotelctl deploy review")
	}
	if options.ActivateOnEnter && in == nil {
		return fmt.Errorf("--activate-on-enter требует ввода из терминала или stdin")
	}
	if options.Direction == "handler" && options.Unavailable && current["demo.handler_abort_supported"] != "1" {
		return fmt.Errorf("обновите Review и Envoy: hotelctl build review && hotelctl deploy review")
	}
	if len(options.DelayProfile) != 0 && current["demo.delay_profile_supported"] != "1" {
		return fmt.Errorf("Envoy ещё не поддерживает --delay-profile; обновите его через hotelctl deploy api-gateway")
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
	rampStartKey := "demo." + options.From + "." + options.To + ".handler.delay_ramp_start_ms"
	initial := values
	if options.ActivateOnEnter {
		baseline := options
		baseline.DelayPerInflight = 0
		initial = baseline.Values()
	} else if options.DelayRamp > 0 {
		values[rampStartKey] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	if err := runtime.Modify(ctx, initial); err != nil {
		return err
	}
	if err := verifyRuntime(ctx, runtime, initial); err != nil {
		return err
	}

	timer := time.NewTimer(options.Duration)
	defer timer.Stop()
	if options.ActivateOnEnter {
		fmt.Fprintf(out, "ACTIVE %s → %s: базовая задержка %s, общее окно %s\n", options.From, options.To, options.Slowdown, options.Duration)
		fmt.Fprintln(out, "Запустите нагрузку. Нажмите Enter, чтобы включить рост задержки по inflight.")
		activation := make(chan error, 1)
		go func() {
			_, readErr := bufio.NewReader(in).ReadString('\n')
			activation <- readErr
		}()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case readErr := <-activation:
			if readErr != nil {
				return fmt.Errorf("не удалось дождаться Enter: %w", readErr)
			}
		}
		values[rampStartKey] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		if err := runtime.Modify(ctx, values); err != nil {
			return err
		}
		if err := verifyRuntime(ctx, runtime, values); err != nil {
			return err
		}
	}
	if len(options.DelayProfile) != 0 {
		fmt.Fprintf(out, "ACTIVE %s → %s: профиль request на %s\n", options.From, options.To, options.Duration)
		total := 0
		for _, step := range options.DelayProfile {
			fmt.Fprintf(out, "  %d%% попыток: +%s\n", step.Percent, step.Delay)
			total += step.Percent
		}
		fmt.Fprintf(out, "  %d%% попыток: без задержки\n", 100-total)
	} else if options.Unavailable {
		fmt.Fprintf(out, "ACTIVE %s → %s: UNAVAILABLE (%s) на %s\n", options.From, options.To, options.Direction, options.Duration)
		if options.PushbackMS != nil {
			fmt.Fprintf(out, "grpc-retry-pushback-ms: %d (ответ до обработки запроса сервисом)\n", *options.PushbackMS)
		}
	} else if options.DelayPerInflight > 0 {
		if options.ActivateOnEnter {
			fmt.Fprintf(out, "INFLIGHT ACTIVE %s → %s\n", options.From, options.To)
		} else {
			fmt.Fprintf(out, "ACTIVE %s → %s: handler по inflight на %s\n", options.From, options.To, options.Duration)
		}
		fmt.Fprintf(out, "Задержка = min(потолок, %s + %s × max(0, inflight − %d))\n", options.Slowdown, options.DelayPerInflight, max(1, options.InflightThreshold))
		if options.DelayRamp > 0 {
			fmt.Fprintf(out, "Потолок растёт от %s до %s за %s.\n", options.Slowdown, options.MaxDelay, options.DelayRamp)
		} else {
			fmt.Fprintf(out, "Потолок задержки: %s.\n", options.MaxDelay)
		}
		fmt.Fprintln(out, "inflight: допущенные GetHotelRatings в одном процессе Review, включая текущий вызов; backlog limiter не учитывается.")
	} else {
		fmt.Fprintf(out, "ACTIVE %s → %s: %s +%s на %s\n", options.From, options.To, options.Direction, options.Slowdown, options.Duration)
	}
	if len(options.DelayProfile) == 0 {
		fmt.Fprintf(out, "Доля попыток со сбоем: %d%%\n", options.Percentage())
	}
	fmt.Fprintln(out, "Отправьте запрос из второго терминала. Ctrl+C досрочно снимает правило.")
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
