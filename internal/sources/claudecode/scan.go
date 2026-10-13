package claudecode

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// group is one parent session plus the sub-agent transcripts that fold into it.
type group struct {
	id     string
	parent string
	subs   []string
	files  []scannedFile
}

type scannedFile struct {
	abs   string
	rel   string
	mtime int64
	size  int64
}

// scanProjects lists parent transcripts under root/projects and the sub-agent
// files that belong to each parent. Symlinks are skipped. A missing config or
// projects directory is a warning, not a failure.
func scanProjects(root string) ([]group, []source.LogLine, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []source.LogLine{warn("claude config dir not found: " + root)}, nil
		}
		return nil, nil, failedErr("cannot read claude config dir: " + root)
	}
	if !info.IsDir() {
		return nil, []source.LogLine{warn("claude config dir is not a directory: " + root)}, nil
	}

	projects := filepath.Join(root, "projects")
	info, err = os.Stat(projects)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []source.LogLine{warn("projects directory not found: " + projects)}, nil
		}
		return nil, nil, failedErr("cannot read projects: " + projects)
	}
	if !info.IsDir() {
		return nil, []source.LogLine{warn("projects path is not a directory: " + projects)}, nil
	}

	entries, err := os.ReadDir(projects)
	if err != nil {
		return nil, nil, failedErr("cannot read projects: " + projects)
	}

	var (
		groups []group
		logs   []source.LogLine
	)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		found, projectLogs := scanProject(root, filepath.Join(projects, entry.Name()))
		groups = append(groups, found...)
		logs = append(logs, projectLogs...)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].sortKey() < groups[j].sortKey()
	})
	return groups, logs, nil
}

func (g group) sortKey() string {
	if len(g.files) > 0 {
		return g.files[0].rel
	}
	return g.id
}

func scanProject(root, dir string) ([]group, []source.LogLine) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []source.LogLine{warn("cannot read project: " + dir)}
	}

	parents := map[string]scannedFile{}
	subs := map[string][]scannedFile{}
	var names []string
	seen := map[string]struct{}{}
	var logs []source.LogLine

	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, name)
		if entry.IsDir() {
			found, subLogs := scanSubagents(root, path)
			logs = append(logs, subLogs...)
			if len(found) == 0 {
				continue
			}
			subs[name] = found
			if _, ok := seen[name]; !ok {
				names = append(names, name)
				seen[name] = struct{}{}
			}
			continue
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if id == "" {
			continue
		}
		file, ok := fileStamp(root, path, entry)
		if !ok {
			logs = append(logs, warn("cannot read session: "+path))
			continue
		}
		parents[id] = file
		if _, exists := seen[id]; !exists {
			names = append(names, id)
			seen[id] = struct{}{}
		}
	}

	sort.Strings(names)
	groups := make([]group, 0, len(names))
	for _, id := range names {
		g := group{id: id}
		if parent, ok := parents[id]; ok {
			g.parent = parent.abs
			g.files = append(g.files, parent)
		}
		children := subs[id]
		sort.Slice(children, func(i, j int) bool { return children[i].rel < children[j].rel })
		for _, child := range children {
			g.subs = append(g.subs, child.abs)
			g.files = append(g.files, child)
		}
		if len(g.files) == 0 {
			continue
		}
		groups = append(groups, g)
	}
	return groups, logs
}

func scanSubagents(root, sessionDir string) ([]scannedFile, []source.LogLine) {
	dir := filepath.Join(sessionDir, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []source.LogLine{warn("cannot read subagents: " + dir)}
	}
	var (
		files []scannedFile
		logs  []source.LogLine
	)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, ok := fileStamp(root, path, entry)
		if !ok {
			logs = append(logs, warn("cannot read session: "+path))
			continue
		}
		files = append(files, file)
	}
	return files, logs
}

func fileStamp(root, path string, entry os.DirEntry) (scannedFile, bool) {
	info, err := entry.Info()
	if err != nil {
		return scannedFile{}, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return scannedFile{}, false
	}
	return scannedFile{
		abs:   path,
		rel:   filepath.ToSlash(rel),
		mtime: info.ModTime().UnixNano(),
		size:  info.Size(),
	}, true
}
