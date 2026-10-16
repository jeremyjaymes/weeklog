package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const fieldSep = "\x1f"

type rawCommit struct {
	sha     string
	name    string
	email   string
	when    time.Time
	branch  string
	subject string
	body    string
}

type diffStat struct {
	files   []string
	added   int
	deleted int
	hasDiff bool
}

func collectRepo(ctx context.Context, repo string, opts options) ([]event.Event, error) {
	remote, err := lookupRemote(ctx, repo)
	if err != nil {
		return nil, failedErr(repo + ": " + oneLine(err.Error()))
	}
	normalized := normalizeRemote(remote)
	commits, err := readCommits(ctx, repo)
	if err != nil {
		return nil, failedErr(repo + ": " + oneLine(err.Error()))
	}
	stats, err := readNumstat(ctx, repo)
	if err != nil {
		return nil, failedErr(repo + ": " + oneLine(err.Error()))
	}

	var events []event.Event
	for _, commit := range commits {
		if commit.when.Before(opts.since) || !commit.when.Before(opts.until) {
			continue
		}
		if !opts.authors.allow(commit.name, commit.email) {
			continue
		}
		ev, err := commitEvent(repo, normalized, commit, stats[commit.sha])
		if err != nil {
			return nil, failedErr(repo + ": " + oneLine(err.Error()))
		}
		events = append(events, ev)
	}
	return events, nil
}

func commitEvent(repo, normalized string, commit rawCommit, stat diffStat) (event.Event, error) {
	summary := commitMessage(commit.subject, commit.body)
	ev := event.Event{
		Source:   sourceName,
		SourceID: commit.sha,
		Kind:     event.KindCommit,
		TS:       commit.when,
		Actor:    commit.email,
		Title:    strings.TrimSpace(commit.subject),
		Summary:  summary,
		URL:      commitURL(normalized, commit.sha),
		Repo:     normalized,
		Branch:   commit.branch,
		CWD:      repo,
		Files:    stat.files,
		Refs:     parseRefs(summary, commit.sha),
	}
	if stat.hasDiff {
		ev.Metrics = map[string]float64{
			"lines_added":   float64(stat.added),
			"lines_deleted": float64(stat.deleted),
		}
	}
	return event.Prepare(ev)
}

func commitMessage(subject, body string) string {
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	switch {
	case subject == "" && body == "":
		return ""
	case body == "":
		return subject
	case subject == "":
		return body
	default:
		return subject + "\n\n" + body
	}
}

func commitURL(repo, sha string) string {
	host, _, ok := strings.Cut(repo, "/")
	if !ok || !strings.Contains(host, ".") || strings.ContainsAny(repo, " \t") {
		return ""
	}
	return "https://" + repo + "/commit/" + sha
}

func readCommits(ctx context.Context, repo string) ([]rawCommit, error) {
	out, err := gitBytes(ctx, repo, "log", "--all", "--source", "-z",
		"--pretty=format:%H%x1f%an%x1f%ae%x1f%aI%x1f%S%x1f%s%x1f%b")
	if err != nil {
		if noCommits(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimRight(out, "\x00")) == 0 {
		return nil, nil
	}
	parts := bytes.Split(out, []byte{0})
	commits := make([]rawCommit, 0, len(parts))
	for _, part := range parts {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}
		commit, err := parseCommit(part)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func parseCommit(part []byte) (rawCommit, error) {
	fields := bytes.Split(part, []byte(fieldSep))
	if len(fields) < 7 {
		return rawCommit{}, errors.New("git log record is incomplete")
	}
	when, err := time.Parse(time.RFC3339, string(fields[3]))
	if err != nil {
		return rawCommit{}, fmt.Errorf("parse author date: %w", err)
	}
	body := string(bytes.Join(fields[6:], []byte(fieldSep)))
	return rawCommit{
		sha:     strings.TrimSpace(string(fields[0])),
		name:    string(fields[1]),
		email:   string(fields[2]),
		when:    when,
		branch:  branchHint(string(fields[4])),
		subject: string(fields[5]),
		body:    body,
	}, nil
}

func branchHint(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		return strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/remotes/"):
		rest := strings.TrimPrefix(ref, "refs/remotes/")
		_, branch, ok := strings.Cut(rest, "/")
		if !ok {
			return ""
		}
		return branch
	default:
		return ""
	}
}

func readNumstat(ctx context.Context, repo string) (map[string]diffStat, error) {
	out, err := gitBytes(ctx, repo, "log", "--all", "-M", "--numstat", "--pretty=format:%x1e%H")
	if err != nil {
		if noCommits(err) {
			return map[string]diffStat{}, nil
		}
		return nil, err
	}
	stats := make(map[string]diffStat)
	var (
		sha  string
		stat diffStat
	)
	flush := func() {
		if sha == "" {
			return
		}
		stats[sha] = stat
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "\x1e") {
			flush()
			sha = strings.TrimSpace(strings.TrimPrefix(line, "\x1e"))
			stat = diffStat{}
			continue
		}
		if sha == "" || strings.TrimSpace(line) == "" {
			continue
		}
		file, added, deleted, ok := parseNumstat(line)
		if !ok {
			continue
		}
		stat.hasDiff = true
		stat.added += added
		stat.deleted += deleted
		if file != "" {
			stat.files = append(stat.files, file)
		}
	}
	flush()
	return stats, nil
}

func parseNumstat(line string) (file string, added, deleted int, ok bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return "", 0, 0, false
	}
	file = canonicalPath(parts[2])
	if parts[0] != "-" {
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			return "", 0, 0, false
		}
		added = n
	}
	if parts[1] != "-" {
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, 0, false
		}
		deleted = n
	}
	return file, added, deleted, file != ""
}

// canonicalPath returns the destination path of a numstat entry.
// Rename entries use git's "old => new" or "prefix/{old => new}suffix" form.
func canonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}
	}
	if strings.Contains(path, " => ") {
		if open := strings.Index(path, "{"); open >= 0 {
			if end := strings.LastIndex(path, "}"); end > open {
				left, right, found := strings.Cut(path[open+1:end], " => ")
				if found && left != "" {
					path = path[:open] + right + path[end+1:]
				}
			}
		} else if _, right, found := strings.Cut(path, " => "); found {
			path = strings.TrimSpace(right)
		}
	}
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return strings.ReplaceAll(path, `\`, "/")
}

func lookupRemote(ctx context.Context, repo string) (string, error) {
	out, err := gitBytes(ctx, repo, "remote")
	if err != nil {
		return "", err
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	chosen := names[0]
	for _, name := range names {
		if name == "origin" {
			chosen = name
			break
		}
	}
	// "--" keeps a remote name that starts with "-" from being read as a
	// get-url option such as --push or --all.
	raw, err := gitBytes(ctx, repo, "remote", "get-url", "--", chosen)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func lookGit() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", failedErr("git is not installed")
	}
	return path, nil
}

func gitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full := make([]string, 0, len(args)+6)
	full = append(full, "--no-pager", "--no-optional-locks", "-c", "core.quotepath=false", "-c", "log.showSignature=false")
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}

func gitEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+4)
	for _, item := range base {
		if strings.HasPrefix(item, "GIT_CONFIG_GLOBAL=") ||
			strings.HasPrefix(item, "GIT_CONFIG_SYSTEM=") ||
			strings.HasPrefix(item, "GIT_CONFIG_NOSYSTEM=") ||
			strings.HasPrefix(item, "GIT_DIR=") ||
			strings.HasPrefix(item, "GIT_WORK_TREE=") {
			continue
		}
		env = append(env, item)
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

func noCommits(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "does not have any commits yet")
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func sortEvents(events []event.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].TS.Equal(events[j].TS) {
			return events[i].TS.Before(events[j].TS)
		}
		return events[i].SourceID < events[j].SourceID
	})
}
