package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// namePattern matches config source types: a letter or digit, then letters,
// digits, underscores, or hyphens.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func validName(name string) bool {
	return namePattern.MatchString(name)
}

// PluginDir returns the directory scanned for weeklog-source-* executables.
// XDG_DATA_HOME/weeklog/plugins is used when XDG_DATA_HOME is set. Otherwise
// the path is ~/.local/share/weeklog/plugins, or
// %USERPROFILE%\AppData\Local\weeklog\plugins on Windows.
func PluginDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "weeklog", "plugins"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find plugin directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", "weeklog", "plugins"), nil
	}
	return filepath.Join(home, ".local", "share", "weeklog", "plugins"), nil
}

// Ignored is a weeklog-source-* file that was not used.
type Ignored struct {
	Name   string
	Path   string
	Reason string
}

type foundPlugin struct {
	Name string
	Path string
}

type pluginDirStatus struct {
	Path    string
	Missing bool
	Err     error
	Warning string
}

func (r *Registry) pluginDirectory() (string, error) {
	if strings.TrimSpace(r.pluginDir) != "" {
		return r.pluginDir, nil
	}
	return PluginDir()
}

func (r *Registry) pathList() string {
	if r.path != nil {
		return *r.path
	}
	return os.Getenv("PATH")
}

// discover finds external plugins. The plugin directory is searched before
// PATH. The first executable for a name wins. Built-in names are not replaced.
func (r *Registry) discover() ([]foundPlugin, []Ignored, pluginDirStatus) {
	var ignored []Ignored
	seenPath := map[string]struct{}{}
	byName := map[string]foundPlugin{}
	var order []string

	status := pluginDirStatus{}
	dir, err := r.pluginDirectory()
	if err != nil {
		status.Err = err
	} else {
		status = inspectPluginDir(dir)
		if status.Err == nil && !status.Missing {
			r.takePlugins(scanDir(dir, &ignored), byName, &order, seenPath, &ignored)
		}
	}

	for _, dir := range filepath.SplitList(r.pathList()) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		r.takePlugins(scanDir(abs, &ignored), byName, &order, seenPath, &ignored)
	}

	found := make([]foundPlugin, 0, len(order))
	for _, name := range order {
		if _, builtin := r.builtins[name]; builtin {
			plugin := byName[name]
			ignored = append(ignored, Ignored{
				Name:   name,
				Path:   plugin.Path,
				Reason: "shadows built-in " + name,
			})
			continue
		}
		found = append(found, byName[name])
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	sort.Slice(ignored, func(i, j int) bool {
		if ignored[i].Path == ignored[j].Path {
			return ignored[i].Reason < ignored[j].Reason
		}
		return ignored[i].Path < ignored[j].Path
	})
	return found, ignored, status
}

func (r *Registry) takePlugins(found []foundPlugin, byName map[string]foundPlugin, order *[]string, seenPath map[string]struct{}, ignored *[]Ignored) {
	for _, plugin := range found {
		clean := fileIdentity(plugin.Path)
		if _, ok := seenPath[clean]; ok {
			continue
		}
		seenPath[clean] = struct{}{}
		if prev, ok := byName[plugin.Name]; ok {
			*ignored = append(*ignored, Ignored{
				Name:   plugin.Name,
				Path:   plugin.Path,
				Reason: "duplicate of " + prev.Path,
			})
			continue
		}
		byName[plugin.Name] = plugin
		*order = append(*order, plugin.Name)
	}
}

func inspectPluginDir(path string) pluginDirStatus {
	status := pluginDirStatus{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Missing = true
			return status
		}
		status.Err = errors.New("cannot read")
		return status
	}
	if !info.IsDir() {
		status.Err = errors.New("not a directory")
		return status
	}
	if !directorySearchable(info) {
		status.Err = errors.New("not searchable")
		return status
	}
	if _, err := os.ReadDir(path); err != nil {
		status.Err = errors.New("cannot read")
		return status
	}
	if !directoryWritable(path) {
		status.Warning = "not writable"
	}
	return status
}

func scanDir(dir string, ignored *[]Ignored) []foundPlugin {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var found []foundPlugin
	for _, fileName := range names {
		name, ok := pluginName(fileName)
		if !ok {
			continue
		}
		path := filepath.Join(dir, fileName)
		info, err := os.Stat(path)
		if err != nil {
			*ignored = append(*ignored, Ignored{Name: name, Path: path, Reason: "cannot stat"})
			continue
		}
		if !info.Mode().IsRegular() {
			*ignored = append(*ignored, Ignored{Name: name, Path: path, Reason: "not a file"})
			continue
		}
		if !executable(info) {
			*ignored = append(*ignored, Ignored{Name: name, Path: path, Reason: "not executable"})
			continue
		}
		if reason := pluginTrustReason(dir, info); reason != "" {
			*ignored = append(*ignored, Ignored{Name: name, Path: path, Reason: reason})
			continue
		}
		found = append(found, foundPlugin{Name: name, Path: path})
	}
	return found
}

func pluginName(file string) (string, bool) {
	name := file
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name[:len(name)-len(".exe")]
	}
	rest, ok := strings.CutPrefix(name, PluginPrefix)
	if !ok || !validName(rest) {
		return "", false
	}
	return rest, true
}

func executable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func fileIdentity(path string) string {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return clean
	}
	return resolved
}
