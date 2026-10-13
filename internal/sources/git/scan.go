package git

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// scanRoots returns repository directories under roots.
// A directory is a repository when it contains a .git entry. The walk includes
// the root and maxScanDepth levels below it, and does not enter a repository.
func scanRoots(roots []string) ([]string, []source.LogLine) {
	seen := make(map[string]struct{})
	var (
		repos []string
		logs  []source.LogLine
	)
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				logs = append(logs, source.LogLine{Level: "warn", Msg: "root not found: " + root})
				continue
			}
			logs = append(logs, source.LogLine{Level: "warn", Msg: "cannot read root: " + root})
			continue
		}
		if !info.IsDir() {
			logs = append(logs, source.LogLine{Level: "warn", Msg: "root is not a directory: " + root})
			continue
		}
		walkRoot(root, 0, seen, &repos, &logs)
	}
	sort.Strings(repos)
	return repos, logs
}

func walkRoot(dir string, depth int, seen map[string]struct{}, repos *[]string, logs *[]source.LogLine) {
	if isRepo(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		*repos = append(*repos, abs)
		return
	}
	if depth >= maxScanDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		*logs = append(*logs, source.LogLine{Level: "warn", Msg: "cannot read directory: " + dir})
		return
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == ".git" {
			continue
		}
		walkRoot(filepath.Join(dir, name), depth+1, seen, repos, logs)
	}
}

func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
