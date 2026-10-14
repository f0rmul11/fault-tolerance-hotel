package healthcheck

import (
	"fmt"
	"net/http"
	"sync"
)

const success = "success"

type CheckFunc func() error

type result struct {
	name   string
	output string
}

func (h *handler) check(checks map[string]CheckFunc, out map[string]string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var (
		status = http.StatusOK
		wg     sync.WaitGroup
	)
	resultChan := make(chan result, len(checks))

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check CheckFunc) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					resultChan <- result{
						name:   name,
						output: fmt.Sprintf("panic recovered: %v", r),
					}
				}
			}()

			var output = success
			if err := check(); err != nil {
				output = err.Error()
			}

			resultChan <- result{
				name:   name,
				output: output,
			}
		}(
			name,
			check,
		)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for res := range resultChan {
		out[res.name] = res.output

		if res.output != success {
			status = http.StatusServiceUnavailable
		}
	}

	return status
}
