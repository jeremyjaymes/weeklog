package codex

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

type scannedFile struct {
	abs   string
	rel   string
	mtime int64
	size  int64
}

// scanSessions lists rollout-*.jsonl files under root/sessions/YYYY/MM/DD.
// Symlinks are skipped. A missing Codex home or sessions directory is a
// warning, not a failure. Other JSONL files, including history.jsonl, are
// left unread.
func scanSessions(root string) ([]scannedFile, []source.LogLine, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []source.LogLine{warn("codex home not found: " + root)}, nil
		}
		return nil, nil, failedErr("cannot read codex home: " + root)
	}
	if !info.IsDir() {
		return nil, []source.LogLine{warn("codex home is not a directory: " + root)}, nil
	}

	sessions := filepath.Join(root, "sessions")
	info, err = os.Stat(sessions)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []source.LogLine{warn("sessions directory not found: " + sessions)}, nil
		}
		return nil, nil, failedErr("cannot read sessions: " + sessions)
	}
	if !info.IsDir() {
		return nil, []source.LogLine{warn("sessions path is not a directory: " + sessions)}, nil
	}

	years, err := readSubdirs(sessions)
	if err != nil {
		return nil, nil, failedErr("cannot read sessions: " + sessions)
	}

	var (
		files []scannedFile
		logs  []source.LogLine
	)
	for _, year := range years {
		if !digits(year, 4) {
			continue
		}
		yearPath := filepath.Join(sessions, year)
		months, err := readSubdirs(yearPath)
		if err != nil {
			logs = append(logs, warn("cannot read sessions: "+yearPath))
			continue
		}
		for _, month := range months {
			if !digits(month, 2) {
				continue
			}
			monthPath := filepath.Join(yearPath, month)
			days, err := readSubdirs(monthPath)
			if err != nil {
				logs = append(logs, warn("cannot read sessions: "+monthPath))
				continue
			}
			for _, day := range days {
				if !digits(day, 2) {
					continue
				}
				dayPath := filepath.Join(monthPath, day)
				found, dayLogs := scanDay(root, dayPath)
				files = append(files, found...)
				logs = append(logs, dayLogs...)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, logs, nil
}

func scanDay(root, dir string) ([]scannedFile, []source.LogLine) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []source.LogLine{warn("cannot read day: " + dir)}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			continue
		}
		if !isRollout(entry.Name()) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var (
		files []scannedFile
		logs  []source.LogLine
	)
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			logs = append(logs, warn("cannot read session: "+path))
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			logs = append(logs, warn("cannot read session: "+path))
			continue
		}
		files = append(files, scannedFile{
			abs:   path,
			rel:   filepath.ToSlash(rel),
			mtime: info.ModTime().UnixNano(),
			size:  info.Size(),
		})
	}
	return files, logs
}

func readSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func isRollout(name string) bool {
	return strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") && len(name) > len("rollout-")+len(".jsonl")
}

func digits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// idFromName returns the session id embedded in a rollout filename.
// rollout-YYYY-MM-DDThh-mm-ss-<id>.jsonl yields <id>. Any other rollout
// name yields the text after the rollout- prefix.
func idFromName(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	rest := strings.TrimPrefix(base, "rollout-")
	if len(rest) > 20 && rest[19] == '-' && stamp(rest[:19]) {
		return rest[20:]
	}
	if rest != "" {
		return rest
	}
	return base
}

func stamp(s string) bool {
	// 2006-01-02T15-04-05
	if len(s) != 19 {
		return false
	}
	for i, r := range s {
		switch i {
		case 4, 7:
			if r != '-' {
				return false
			}
		case 10:
			if r != 'T' {
				return false
			}
		case 13, 16:
			if r != '-' {
				return false
			}
		default:
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
