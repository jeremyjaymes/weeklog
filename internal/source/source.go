package source

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const (
	// ProtocolVersion is the JSON protocol this package speaks.
	ProtocolVersion = 1

	// DefaultTimeout is the time limit for one plugin invocation.
	DefaultTimeout = 120 * time.Second

	// BuiltinVersion is the describe version of a built-in source whose
	// collector is not implemented yet.
	BuiltinVersion = "0.0.0"

	// PluginPrefix is the executable name prefix for an external source.
	PluginPrefix = "weeklog-source-"

	// ExitOK means the plugin finished.
	ExitOK = 0
	// ExitFailed means the plugin failed. Stderr is part of the error.
	ExitFailed = 1
	// ExitConfig means the plugin rejected its configuration or the request.
	ExitConfig = 2
)

// Config field types advertised by describe.
const (
	TypeString  = "string"
	TypeStrings = "strings"
	TypeSecret  = "secret"
)

var (
	// ErrNotImplemented means the source has no collector yet.
	ErrNotImplemented = errors.New("collector is not implemented")
	// ErrConfig means the plugin rejected its configuration or the request.
	ErrConfig = errors.New("plugin config error")
	// ErrFailed means the plugin exited with a failure.
	ErrFailed = errors.New("plugin failed")
	// ErrTimeout means the plugin exceeded its time limit.
	ErrTimeout = errors.New("plugin timed out")
	// ErrProtocol means the plugin spoke protocol v1 incorrectly.
	ErrProtocol = errors.New("plugin protocol error")
)

// Source collects events for one activity provider.
// Describe reports the protocol version and config schema.
// Collect emits events for the requested window. A cursor, when the source
// has one, is returned on Result rather than as an event.
type Source interface {
	Describe(ctx context.Context) (Manifest, error)
	Collect(ctx context.Context, req CollectRequest, emit func(event.Event) error) (Result, error)
}

// Manifest is the describe document.
type Manifest struct {
	Protocol int              `json:"protocol"`
	Name     string           `json:"name"`
	Version  string           `json:"version"`
	Config   map[string]Field `json:"config"`
}

// Field is one config key a source accepts.
type Field struct {
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// Identity is the developer identity forwarded to a plugin.
// Handles uses provider names such as "github".
type Identity struct {
	Emails  []string          `json:"emails"`
	Handles map[string]string `json:"handles"`
}

// CollectRequest is the collect command's stdin document.
// Cursor is nil when the host has no saved cursor. Config holds resolved
// settings, including secrets. Callers must not log Config.
type CollectRequest struct {
	Protocol int            `json:"protocol"`
	Since    time.Time      `json:"since"`
	Until    time.Time      `json:"until"`
	Cursor   *string        `json:"cursor"`
	Config   map[string]any `json:"config"`
	Identity Identity       `json:"identity"`
}

// LogLine is a diagnostic from a source. The text is untrusted.
type LogLine struct {
	Level string
	Msg   string
}

// Result is what a collect run reports besides events.
// HasCursor is false when the source did not send a cursor; the host should
// keep the previous cursor. Dropped counts invalid lines that were ignored.
type Result struct {
	Cursor    string
	HasCursor bool
	Logs      []LogLine
	Dropped   int
	Events    int
}

// Failure is a plugin or protocol error. Error text never includes the
// collect request. Stderr is included because a failed plugin's stderr is
// shown to the user; plugins must not print secrets there.
type Failure struct {
	Name   string
	Op     string
	Code   int
	Stderr string
	Reason string
	Err    error
}

func (f *Failure) Error() string {
	if f == nil {
		return "plugin failed"
	}
	msg := f.Reason
	if msg == "" {
		msg = "failed"
	}
	if f.Name != "" && f.Op != "" {
		msg = f.Name + ": " + f.Op + ": " + msg
	} else if f.Name != "" {
		msg = f.Name + ": " + msg
	}
	if f.Code > 0 {
		msg += fmt.Sprintf(" (exit %d)", f.Code)
	}
	if note := oneLine(truncate(f.Stderr, 200)); note != "" {
		msg += ": " + note
	}
	return msg
}

// Unwrap returns the sentinel this failure matches.
func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Err
}

// Unimplemented is a Collect implementation for a source that can describe
// itself and does not collect yet. Embed it and add Describe.
type Unimplemented struct {
	Name string
}

// Collect returns ErrNotImplemented.
func (u Unimplemented) Collect(ctx context.Context, _ CollectRequest, _ func(event.Event) error) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	name := u.Name
	if name == "" {
		name = "source"
	}
	return Result{}, fmt.Errorf("%s: %w", name, ErrNotImplemented)
}
