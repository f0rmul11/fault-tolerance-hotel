package healthcheck

import (
	"encoding/json"
	"net/http"
	"sync"
)

const (
	LivenessPath  = "/healthcheck/live"
	ReadinessPath = "/healthcheck/ready"
	StartupPath   = "/healthcheck/startup"
)

type Handler interface {
	http.Handler

	LiveEndpoint(w http.ResponseWriter, r *http.Request)
	ReadyEndpoint(w http.ResponseWriter, r *http.Request)
	StartupEndpoint(w http.ResponseWriter, r *http.Request)

	AddReadinessCheck(name string, check CheckFunc)
	AddLivenessCheck(name string, check CheckFunc)
	AddStartupCheck(name string, check CheckFunc)
}

type handler struct {
	http.ServeMux
	mu              sync.RWMutex
	livenessChecks  map[string]CheckFunc
	readinessChecks map[string]CheckFunc
	startupChecks   map[string]CheckFunc
}

func NewHandler() Handler {
	h := &handler{
		livenessChecks:  make(map[string]CheckFunc),
		readinessChecks: make(map[string]CheckFunc),
		startupChecks:   make(map[string]CheckFunc),
	}

	h.Handle(LivenessPath, http.HandlerFunc(h.LiveEndpoint))
	h.Handle(ReadinessPath, http.HandlerFunc(h.ReadyEndpoint))
	h.Handle(StartupPath, http.HandlerFunc(h.StartupEndpoint))

	return h
}

func (h *handler) LiveEndpoint(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, h.livenessChecks)
}

func (h *handler) ReadyEndpoint(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, h.readinessChecks, h.livenessChecks)
}

func (h *handler) StartupEndpoint(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, h.startupChecks)
}

func (h *handler) AddReadinessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readinessChecks[name] = check
}

func (h *handler) AddLivenessCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.livenessChecks[name] = check
}

func (h *handler) AddStartupCheck(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startupChecks[name] = check
}

func (h *handler) handle(w http.ResponseWriter, r *http.Request, checks ...map[string]CheckFunc) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	checkResults := make(map[string]string)
	status := http.StatusOK

	for _, m := range checks {
		if st := h.check(m, checkResults); st != http.StatusOK {
			status = st
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "		")
	_ = encoder.Encode(checkResults)
}
