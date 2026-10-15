// Package rtconfig maintains a validated, immutable view of service configuration.
// Reads never contact etcd; a single background watch publishes whole snapshots.
package rtconfig

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	maxKeys          = 256
	maxKeyBytes      = 128
	maxValueBytes    = 1024
	maxSnapshotBytes = 256 << 10
	maxObservers     = 64
	maxReceiveBytes  = 1 << 20
)

var (
	ErrVariableNotFound = errors.New("rtconfig: variable not found")
	ErrNoPreviousValue  = errors.New("rtconfig: no previous value")
	ErrNotInitialized   = errors.New("rtconfig: client is not initialized")
	ErrNotStarted       = errors.New("rtconfig: manager is not started")
	ErrAlreadyStarted   = errors.New("rtconfig: manager is already started")
	ErrClosed           = errors.New("rtconfig: manager is closed")
	ErrObserverLimit    = errors.New("rtconfig: observer limit reached")
	ErrCallbackPanic    = errors.New("rtconfig: callback panicked")
	ErrSnapshotLimit    = errors.New("rtconfig: configuration exceeds size limits")
)

// Client is the scratch-compatible consumer API. Values are immutable and safe
// to retain. Use Values to read several keys from the same generation.
type Client interface {
	Value(context.Context, string) (Value, error)
	Values(context.Context) (map[string]Value, error)
	Version() Version
	Ready(context.Context) <-chan error
	WatchVariable(context.Context, string, VariableChangeCallback) error
	WatchAllVariables(context.Context, VariableChangeCallback) error
}

// Variable identifies a value delivered to an observer. Initial old values carry
// ErrNoPreviousValue rather than a nil interface.
type Variable interface {
	Name() string
	Value() Value
}

type variable struct {
	name  string
	value Value
}

func (v variable) Name() string {
	return v.name
}

func (v variable) Value() Value {
	return v.value
}

// VariableChangeCallback receives state, not an event log. Intermediate updates
// may coalesce. Callbacks must be short and must not synchronously close Manager.
type VariableChangeCallback func(old, new Variable)

// Version describes an effective local snapshot, not remote freshness.
type Version interface {
	ID() string
	ProjectID() string
	IsReady() bool
}

type version struct {
	id      string
	project string
}

func (v version) ID() string {
	return v.id
}

func (v version) ProjectID() string {
	return v.project
}

func (v version) IsReady() bool {
	return true
}

// Options contains static connection and recovery settings. Zero durations use
// conservative defaults. TLS is cloned by New; credentials never appear in Status.
type Options struct {
	Endpoints      []string
	Prefix         string
	ProjectID      string
	TLS            *tls.Config
	Username       string
	Password       string
	StartupTimeout time.Duration
	RequestTimeout time.Duration
	WatchTimeout   time.Duration
	ProbeInterval  time.Duration
	ProbeTimeout   time.Duration
	StaleAfter     time.Duration
	RetryMin       time.Duration
	RetryMax       time.Duration
}

func (o Options) normalized() (Options, error) {
	if len(o.Endpoints) == 0 {
		return o, errors.New("rtconfig: at least one etcd endpoint is required")
	}

	for _, endpoint := range o.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return o, errors.New("rtconfig: invalid etcd endpoint URL")
		}
		if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return o, errors.New("rtconfig: endpoint must be an http(s) URL with a host")
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return o, errors.New("rtconfig: endpoint must be an http(s) URL without credentials or path")
		}
	}

	if !strings.HasPrefix(o.Prefix, "/") || !strings.HasSuffix(o.Prefix, "/") || o.Prefix == "/" || strings.Contains(o.Prefix, "//") {
		return o, errors.New("rtconfig: a non-root absolute prefix ending in / is required")
	}

	for _, part := range strings.Split(o.Prefix, "/") {
		if part == "." || part == ".." {
			return o, errors.New("rtconfig: prefix must not contain relative path components")
		}
	}

	defaults := []struct {
		value *time.Duration
		def   time.Duration
	}{
		{
			value: &o.StartupTimeout,
			def:   5 * time.Second,
		},
		{
			value: &o.RequestTimeout,
			def:   3 * time.Second,
		},
		{
			value: &o.WatchTimeout,
			def:   5 * time.Second,
		},
		{
			value: &o.ProbeInterval,
			def:   30 * time.Second,
		},
		{
			value: &o.ProbeTimeout,
			def:   5 * time.Second,
		},
		{
			value: &o.StaleAfter,
			def:   90 * time.Second,
		},
		{
			value: &o.RetryMin,
			def:   250 * time.Millisecond,
		},
		{
			value: &o.RetryMax,
			def:   30 * time.Second,
		},
	}

	for _, item := range defaults {
		if *item.value < 0 {
			return o, errors.New("rtconfig: durations must not be negative")
		}
		if *item.value == 0 {
			*item.value = item.def
		}
	}

	if o.RetryMax < o.RetryMin {
		return o, errors.New("rtconfig: retry maximum must not be less than minimum")
	}

	if o.ProjectID == "" {
		o.ProjectID = strings.Trim(o.Prefix, "/")
	}

	o.Endpoints = append([]string(nil), o.Endpoints...)
	if o.TLS != nil {
		o.TLS = o.TLS.Clone()
	}

	return o, nil
}

// Status is a coherent, local diagnostic copy. Connected and LastConfirmed are
// transport properties; Invalid and AppliedRevision describe effective values.
type Status struct {
	Source           string
	Connected        bool
	Invalid          bool
	Closed           bool
	Stale            bool
	ClusterID        uint64
	AppliedClusterID uint64
	ObservedRevision int64
	AppliedRevision  int64
	Generation       uint64
	LastConfirmed    time.Time
	LastError        string
	Observers        []ObserverStatus
}

// ObserverStatus deliberately distinguishes delivered effects from publication.
type ObserverStatus struct {
	InitialGeneration   uint64
	CompletedGeneration uint64
	PendingGeneration   uint64
	Running             bool
	LastError           string
}

func revisionID(cluster, generation uint64, revision int64) string {
	return fmt.Sprintf("%x:%d:%d", cluster, revision, generation)
}
