package source

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind is builtin or plugin.
type Kind string

const (
	// KindBuiltin is compiled into weeklog.
	KindBuiltin Kind = "builtin"
	// KindPlugin is an external executable.
	KindPlugin Kind = "plugin"
)

// Info is one source after a describe check.
type Info struct {
	Name     string
	Kind     Kind
	Path     string
	Protocol int
	Version  string
	Err      error
}

// Location is the place shown for a source.
func (i Info) Location() string {
	if i.Kind == KindBuiltin {
		return "built-in"
	}
	if i.Path == "" {
		return "-"
	}
	return i.Path
}

// Status is "ok" or "error".
func (i Info) Status() string {
	if i.Err != nil {
		return "error"
	}
	return "ok"
}

// ListResult is the outcome of listing sources.
type ListResult struct {
	PluginDir  string
	DirWarning string
	DirError   string
	Sources    []Info
	Ignored    []Ignored
}

// Registry holds built-in sources and discovers external plugins.
// Register every built-in before List, Open, or Doctor.
// A Registry is not safe for concurrent Register and List calls.
type Registry struct {
	timeout   time.Duration
	pluginDir string
	path      *string
	order     []string
	builtins  map[string]Source
}

// NewRegistry returns an empty registry with the default plugin timeout.
func NewRegistry() *Registry {
	return &Registry{
		timeout:  DefaultTimeout,
		builtins: map[string]Source{},
	}
}

// SetTimeout sets the time limit for one plugin invocation.
// A non-positive duration selects DefaultTimeout.
func (r *Registry) SetTimeout(d time.Duration) {
	if d <= 0 {
		d = DefaultTimeout
	}
	r.timeout = d
}

// SetPluginDir overrides the plugin directory. An empty dir restores the default.
func (r *Registry) SetPluginDir(dir string) {
	r.pluginDir = dir
}

// SetPath overrides PATH used for discovery. The value uses the OS list separator.
func (r *Registry) SetPath(path string) {
	r.path = &path
}

// Register adds a built-in source under name.
func (r *Registry) Register(name string, src Source) error {
	if !validName(name) {
		return fmt.Errorf("invalid source name %q", name)
	}
	if src == nil {
		return errors.New("source is nil")
	}
	if _, ok := r.builtins[name]; ok {
		return fmt.Errorf("source %s is already registered", name)
	}
	if r.builtins == nil {
		r.builtins = map[string]Source{}
	}
	r.builtins[name] = src
	r.order = append(r.order, name)
	return nil
}

// List describes every built-in and discovered plugin.
func (r *Registry) List(ctx context.Context) (ListResult, error) {
	snap, err := r.inspect(ctx)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{
		PluginDir:  snap.dir.Path,
		DirWarning: snap.dirWarning(),
		DirError:   snap.dirError(),
		Sources:    snap.sources,
		Ignored:    snap.ignored,
	}, nil
}

// Open returns the built-in or plugin source named name.
// The returned plugin speaks protocol v1 over a subprocess.
func (r *Registry) Open(name string) (Source, error) {
	if src, ok := r.builtins[name]; ok {
		return src, nil
	}
	if !validName(name) {
		return nil, fmt.Errorf("invalid source name %q", name)
	}
	found, _, status := r.discover()
	if status.Err != nil && status.Path == "" {
		return nil, status.Err
	}
	for _, plugin := range found {
		if plugin.Name == name {
			return &Process{Name: plugin.Name, Path: plugin.Path, Timeout: r.timeoutOr()}, nil
		}
	}
	return nil, fmt.Errorf("source %s not found", name)
}

func (r *Registry) timeoutOr() time.Duration {
	if r.timeout <= 0 {
		return DefaultTimeout
	}
	return r.timeout
}

type inspection struct {
	dir     pluginDirStatus
	sources []Info
	ignored []Ignored
}

func (s inspection) dirWarning() string {
	if s.dir.Err != nil {
		return ""
	}
	if s.dir.Missing {
		return "not found"
	}
	return s.dir.Warning
}

func (s inspection) dirError() string {
	if s.dir.Err == nil {
		return ""
	}
	return s.dir.Err.Error()
}

func (r *Registry) inspect(ctx context.Context) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	var snap inspection
	for _, name := range r.order {
		if err := ctx.Err(); err != nil {
			return inspection{}, err
		}
		snap.sources = append(snap.sources, r.describe(ctx, name, KindBuiltin, "", r.builtins[name]))
	}
	found, ignored, status := r.discover()
	snap.dir = status
	snap.ignored = ignored
	for _, plugin := range found {
		if err := ctx.Err(); err != nil {
			return inspection{}, err
		}
		src := &Process{Name: plugin.Name, Path: plugin.Path, Timeout: r.timeoutOr()}
		snap.sources = append(snap.sources, r.describe(ctx, plugin.Name, KindPlugin, plugin.Path, src))
	}
	return snap, nil
}

func (r *Registry) describe(ctx context.Context, name string, kind Kind, path string, src Source) Info {
	info := Info{Name: name, Kind: kind, Path: path}
	ctx, cancel := context.WithTimeout(ctx, r.timeoutOr())
	defer cancel()
	manifest, err := src.Describe(ctx)
	if err != nil {
		info.Err = err
		return info
	}
	if manifest.Name != name {
		info.Err = &Failure{
			Name:   name,
			Op:     "describe",
			Reason: "manifest name " + oneLine(manifest.Name),
			Err:    ErrProtocol,
		}
		return info
	}
	info.Protocol = manifest.Protocol
	info.Version = manifest.Version
	return info
}
