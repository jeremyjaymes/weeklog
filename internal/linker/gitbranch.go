package linker

import (
	"regexp"
	"strings"
)

// gitInvocation finds one git command and the arguments that follow it on
// the same line. The command may be indented or wrapped in punctuation.
var gitInvocation = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_./-])git\b([^\n;]*)`)

// lastGitBranch returns the last branch named by git checkout or git switch.
// Detached HEAD names and empty commands return "".
func lastGitBranch(text string) string {
	branch := ""
	for _, match := range gitInvocation.FindAllStringSubmatch(text, -1) {
		if next := branchFromGitArgs(match[1]); next != "" {
			branch = next
		}
	}
	return branch
}

func branchFromGitArgs(args string) string {
	fields := strings.Fields(args)
	i := skipGitGlobals(fields)
	if i >= len(fields) {
		return ""
	}
	cmd := strings.ToLower(fields[i])
	if cmd != "checkout" && cmd != "switch" {
		return ""
	}
	i++
	for i < len(fields) {
		f := fields[i]
		switch {
		case f == "--":
			if i+1 >= len(fields) {
				return ""
			}
			return cleanInferredBranch(fields[i+1])
		case isBranchFlag(f):
			if i+1 >= len(fields) {
				return ""
			}
			return cleanInferredBranch(fields[i+1])
		case strings.HasPrefix(f, "-"):
			i++
		default:
			return cleanInferredBranch(f)
		}
	}
	return ""
}

func skipGitGlobals(fields []string) int {
	i := 0
	for i < len(fields) {
		f := fields[i]
		if f == "--" || !strings.HasPrefix(f, "-") {
			return i
		}
		switch f {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace":
			i += 2
		default:
			i++
		}
	}
	return i
}

func isBranchFlag(f string) bool {
	switch f {
	case "-b", "-B", "-c", "-C", "--orphan":
		return true
	default:
		return false
	}
}

func cleanInferredBranch(branch string) string {
	branch = strings.Trim(branch, "\"'`")
	branch = strings.TrimRight(branch, ".,:;")
	branch = normBranch(branch)
	switch branch {
	case "", "HEAD", "-", ".", "..":
		return ""
	}
	if shaRef.MatchString(strings.ToLower(branch)) {
		return ""
	}
	return branch
}
