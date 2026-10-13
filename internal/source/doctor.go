package source

import (
	"context"
	"fmt"
	"strings"
)

// Configured is one source table from a loaded config file.
type Configured struct {
	Index int
	Name  string
}

// DoctorOptions selects the config facts doctor should report.
// Configured is ignored when the file is missing or failed to load.
// Doctor does not resolve secrets and does not read source settings.
type DoctorOptions struct {
	ConfigPath    string
	ConfigMissing bool
	ConfigErr     error
	Configured    []Configured
}

// Report is the doctor check output.
type Report struct {
	text     string
	errors   int
	warnings int
}

// OK reports whether the check has no errors. Warnings still return true.
func (r Report) OK() bool {
	return r.errors == 0
}

// Text returns the human-readable report, including the summary line.
func (r Report) Text() string {
	return r.text
}

// Doctor checks the plugin directory, describe responses, and configured names.
func (r *Registry) Doctor(ctx context.Context, opts DoctorOptions) (Report, error) {
	snap, err := r.inspect(ctx)
	if err != nil {
		return Report{}, err
	}
	var b strings.Builder
	errors, warnings := 0, 0
	add := func(severity, line string) {
		fmt.Fprintf(&b, "%s\n", line)
		switch severity {
		case "error":
			errors++
		case "warning":
			warnings++
		}
	}

	switch {
	case opts.ConfigErr != nil:
		path := opts.ConfigPath
		if path == "" {
			path = "config"
		}
		add("error", "config: "+oneLine(path)+": "+oneLine(opts.ConfigErr.Error()))
	case opts.ConfigMissing:
		add("warning", "config: "+oneLine(opts.ConfigPath)+" (not found)")
	default:
		add("info", "config: "+oneLine(opts.ConfigPath))
	}

	switch {
	case snap.dir.Err != nil && snap.dir.Path == "":
		add("error", "plugin dir: "+oneLine(snap.dir.Err.Error()))
	case snap.dir.Err != nil:
		add("error", "plugin dir: "+oneLine(snap.dir.Path)+": "+oneLine(snap.dir.Err.Error()))
	case snap.dir.Missing:
		add("warning", "plugin dir: "+oneLine(snap.dir.Path)+" (not found)")
	case snap.dir.Warning != "":
		add("warning", "plugin dir: "+oneLine(snap.dir.Path)+" ("+oneLine(snap.dir.Warning)+")")
	default:
		add("info", "plugin dir: "+oneLine(snap.dir.Path))
	}

	for _, item := range snap.ignored {
		add("warning", "ignored: "+oneLine(item.Path)+" ("+oneLine(item.Reason)+")")
	}

	byName := make(map[string]Info, len(snap.sources))
	for _, info := range snap.sources {
		byName[info.Name] = info
		if info.Err != nil {
			add("error", "source "+oneLine(info.Name)+": error: "+oneLine(info.Err.Error()))
			continue
		}
		where := string(info.Kind)
		line := fmt.Sprintf("source %s: ok (%s, protocol %d, version %s", oneLine(info.Name), where, info.Protocol, oneLine(info.Version))
		if info.Kind == KindPlugin {
			line += ", " + oneLine(info.Path)
		}
		line += ")"
		add("info", line)
	}

	if opts.ConfigErr == nil && !opts.ConfigMissing {
		for _, cfg := range opts.Configured {
			info, ok := byName[cfg.Name]
			if ok && info.Err == nil {
				continue
			}
			if ok && info.Err != nil {
				continue
			}
			add("error", fmt.Sprintf("source[%d] %s: error: plugin not found", cfg.Index, oneLine(cfg.Name)))
		}
	}

	fmt.Fprintf(&b, "%s\n", summaryLine(errors, warnings))
	return Report{text: b.String(), errors: errors, warnings: warnings}, nil
}

func summaryLine(errors, warnings int) string {
	switch {
	case errors == 0 && warnings == 0:
		return "ok"
	case errors == 0:
		return plural(warnings, "warning")
	case warnings == 0:
		return plural(errors, "error")
	default:
		return plural(errors, "error") + ", " + plural(warnings, "warning")
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// FormatIgnored renders plugins that were not loaded, including the reason.
func FormatIgnored(items []Ignored) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "ignored: %s (%s)\n", oneLine(item.Path), oneLine(item.Reason))
	}
	return b.String()
}

// FormatSources renders a source list. An error is printed on the following line.
func FormatSources(list []Info) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NAME\tKIND\tSTATUS\tVERSION\tLOCATION\n")
	for _, info := range list {
		version := info.Version
		if version == "" {
			version = "-"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			oneLine(info.Name),
			info.Kind,
			info.Status(),
			oneLine(version),
			oneLine(info.Location()),
		)
		if info.Err != nil {
			fmt.Fprintf(&b, "  %s\n", oneLine(info.Err.Error()))
		}
	}
	return b.String()
}
