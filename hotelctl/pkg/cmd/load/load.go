package load

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/gofrs/flock"
	"github.com/spf13/cobra"
)

const revision = "ae45a6eb6a20f744f5fcc7d606e7a8c66c591d2c"

type options struct {
	addr, method, data, dataFile, reportDir string
	headers, protos, imports                []string
	rps, clients                            int
	duration                                time.Duration
}

func NewCmdLoad(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := &options{}
	cmd := &cobra.Command{
		Use: "load", Short: "gRPC-нагрузка через Framer: endpoint, JSON, RPS и длительность",
		Long: "Постоянная нагрузка на unary gRPC через Framer. Plaintext, timeout запроса 11s.\nReflection включена по умолчанию; без неё используйте --proto и --import-path.\nКаждый запрос повторяет заданное тело. Автоматических ретраев запросов нет.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, metadata, err := o.validate()
			if err != nil {
				return err
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			return o.run(cmd.Context(), cfg.Root, streams, payload, metadata)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&o.addr, "addr", "", "gRPC endpoint host:port (без TLS)")
	flags.StringVar(&o.method, "method", "", "Полное имя package.Service/Method")
	flags.StringVar(&o.data, "data", "{}", "JSON-тело запроса")
	flags.StringVar(&o.dataFile, "data-file", "", "Файл с JSON-телом")
	flags.IntVar(&o.rps, "rps", 0, "Общий RPS для всех соединений")
	flags.DurationVar(&o.duration, "duration", 0, "Длительность подачи нагрузки, например 30s или 5m")
	flags.IntVar(&o.clients, "clients", 1, "Число gRPC-соединений")
	flags.StringArrayVarP(&o.headers, "header", "H", nil, "gRPC metadata key:value, можно повторять")
	flags.StringArrayVar(&o.protos, "proto", nil, "Proto-файл вместо reflection, можно повторять")
	flags.StringArrayVar(&o.imports, "import-path", nil, "Каталог proto imports, можно повторять")
	flags.StringVar(&o.reportDir, "report-dir", "", "Родительский каталог отчётов (по умолчанию .local/hotelctl/load)")
	cmd.MarkFlagsMutuallyExclusive("data", "data-file")
	return cmd
}

func (o *options) validate() (json.RawMessage, map[string][]string, error) {
	if host, port, err := net.SplitHostPort(o.addr); err != nil || host == "" || port == "" {
		return nil, nil, fmt.Errorf("--addr должен иметь вид host:port")
	}
	_, port, _ := net.SplitHostPort(o.addr)
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, nil, fmt.Errorf("некорректный порт")
	}
	o.method = strings.TrimPrefix(o.method, "/")
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*/[A-Za-z_][A-Za-z0-9_]*$`).MatchString(o.method) {
		return nil, nil, fmt.Errorf("--method должен иметь вид package.Service/Method")
	}
	if o.rps < 1 || o.rps > 1000000 {
		return nil, nil, fmt.Errorf("--rps должен быть от 1 до 1000000")
	}
	if o.duration <= 0 {
		return nil, nil, fmt.Errorf("--duration должен быть положительным")
	}
	if o.clients < 1 || o.clients > 1024 {
		return nil, nil, fmt.Errorf("--clients должен быть от 1 до 1024")
	}
	if o.duration > time.Duration(math.MaxInt64)-15*time.Second {
		return nil, nil, fmt.Errorf("слишком большая длительность")
	}
	payload := []byte(o.data)
	if o.dataFile != "" {
		payload, err = os.ReadFile(o.dataFile)
		if err != nil {
			return nil, nil, err
		}
	}
	var object map[string]json.RawMessage
	if len(payload) > 1<<20 || json.Unmarshal(payload, &object) != nil || object == nil {
		return nil, nil, fmt.Errorf("тело должно быть JSON-объектом размером до 1 MiB")
	}
	metadata := map[string][]string{}
	for _, header := range o.headers {
		key, value, ok := strings.Cut(header, ":")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || !regexp.MustCompile(`^[a-z0-9_.-]+$`).MatchString(key) || strings.HasPrefix(key, "grpc-") {
			return nil, nil, fmt.Errorf("неверный ключ metadata")
		}
		metadata[key] = append(metadata[key], strings.TrimSpace(value))
	}
	// Framer does not send :authority; grpc-go also accepts the Host header.
	if len(metadata["host"]) == 0 {
		metadata["host"] = []string{o.addr}
	}
	if len(o.protos) == 0 && len(o.imports) > 0 {
		return nil, nil, fmt.Errorf("--import-path требует --proto")
	}
	return payload, metadata, nil
}

func ensureFramer(ctx context.Context, root string, out io.Writer) (string, error) {
	bin := filepath.Join(root, ".local", "hotelctl", "tools", "framer", revision, "framer")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		return "", err
	}
	lock := flock.New(bin + ".lock")
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return "", err
	}
	if !locked {
		return "", fmt.Errorf("не удалось заблокировать установку Framer")
	}
	defer lock.Unlock()
	if info, err := os.Stat(bin); err == nil && info.Mode()&0111 != 0 {
		return bin, nil
	}
	fmt.Fprintln(out, "Установка закреплённой версии Framer (первый запуск)…")
	installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(installCtx, "bash", filepath.Join(root, "tools", "install-framer.sh"), bin)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("установка Framer: %w", err)
	}
	return bin, nil
}

func (o *options) run(ctx context.Context, root string, s iostreams.IOStreams, payload json.RawMessage, metadata map[string][]string) error {
	bin, err := ensureFramer(ctx, root, s.ErrOut)
	if err != nil {
		return err
	}
	parent := o.reportDir
	if parent == "" {
		parent = filepath.Join(root, ".local", "hotelctl", "load")
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(parent, time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return err
	}
	input := filepath.Join(dir, "requests.json")
	binary := filepath.Join(dir, "requests.bin")
	report := filepath.Join(dir, "phout.tsv")
	defer os.Remove(input)
	defer os.Remove(binary)
	ammo, err := json.Marshal(map[string]any{
		"tag":      "request",
		"call":     strings.Replace(o.method, "/", ".", 1),
		"metadata": metadata,
		"payload":  payload,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(input, append(ammo, '\n'), 0600); err != nil {
		return err
	}
	args := []string{"convert", "--from=ozon.json", "--to=ozon.binary"}
	if len(o.protos) == 0 {
		args = append(args, "--reflection-addr="+o.addr)
	} else {
		for _, p := range o.protos {
			args = append(args, "--reflection-proto="+p)
		}
		for _, p := range o.imports {
			args = append(args, "--reflection-import-path="+p)
		}
	}
	args = append(args, input, binary)
	prepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	convert := exec.CommandContext(prepCtx, bin, args...)
	var diagnostic bytes.Buffer
	convert.Stdout = &diagnostic
	convert.Stderr = &diagnostic
	err = convert.Run()
	cancel()
	if err != nil {
		return fmt.Errorf("подготовка gRPC-запроса: %w: %s", err, diagnostic.String())
	}
	args = []string{"load", "--addr=" + o.addr, "--requests-file=" + binary, "--inmem-requests", "--clients=" + strconv.Itoa(o.clients), "--phout=" + report, "const", strconv.Itoa(o.rps), "--duration=" + o.duration.String()}
	fmt.Fprintf(s.Out, "gRPC  %s  →  %s\nRPS: %d · duration: %s · connections: %d\nОтчёт: %s\n\n", o.addr, o.method, o.rps, o.duration, o.clients, dir)
	fmt.Fprintln(s.Out, "TIME      SENT/s   OK/s   ERR/s   ERROR %")
	loadCtx, stop := context.WithTimeout(ctx, o.duration+15*time.Second)
	defer stop()
	proc := exec.CommandContext(loadCtx, bin, args...)
	proc.Stderr = s.ErrOut
	pipe, err := proc.StdoutPipe()
	if err != nil {
		return err
	}
	if err := proc.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(pipe)
	total := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "total" {
			total = true
			fmt.Fprintln(s.Out, "ИТОГО     SENT     OK     ERR     ERROR %")
			continue
		}
		fields := map[string]string{}
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if ok {
				fields[k] = v
			}
		}
		ok, _ := strconv.ParseUint(fields["ok"], 10, 64)
		failed, _ := strconv.ParseUint(fields["nook"], 10, 64)
		ratio := float64(0)
		if ok+failed > 0 {
			ratio = 100 * float64(failed) / float64(ok+failed)
		}
		if fields["ok"] != "" {
			sent := fields["req/s"]
			if total {
				sent = fields["req"]
			}
			fmt.Fprintf(s.Out, "%s  %-8s %-6d %-7d %.1f%%\n", time.Now().Format("15:04:05"), sent, ok, failed, ratio)
		}
	}
	waitErr := proc.Wait()
	if ctx.Err() != nil {
		fmt.Fprintln(s.Out, "Нагрузка остановлена. Отчёт может быть неполным.")
		return nil
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("Framer завершился с ошибкой; отчёт %s: %w", dir, waitErr)
	}
	if loadCtx.Err() != nil {
		return loadCtx.Err()
	}
	if err := printReport(report, s.Out); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "Сохранён отчёт Framer: %s\n", report)
	return nil
}

// printReport streams the report so memory does not grow with test duration.
func printReport(path string, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	counts := map[string]int64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 12 {
			return fmt.Errorf("некорректная строка отчёта Framer")
		}
		code := fields[11]
		if fields[10] != "0" {
			code += " (errno=" + fields[10] + ")"
		}
		counts[code]++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	keys := make([]string, 0, len(counts))
	for code := range counts {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	for _, code := range keys {
		fmt.Fprintf(out, "  %s: %d\n", code, counts[code])
	}
	return nil
}
