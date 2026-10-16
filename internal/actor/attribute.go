package actor

import (
	"regexp"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Event is the evidence Attribute reads from one stored event.
// Reason is the linker reason for that event inside the work item.
// A loose event has an empty Reason.
type Event struct {
	Kind    event.Kind
	Source  string
	Actor   string
	Title   string
	Summary string
	Branch  string
	Reason  string
}

// sessionSources maps an agent_session source to an actor.
// A source that is not listed is an agent weeklog cannot name.
var sessionSources = map[string]string{
	"claude-code":  Claude,
	"claude":       Claude,
	"codex":        Codex,
	"cursor-cloud": CloudAgent,
}

// branchPrefixes maps the first segment of a branch to an actor.
// The segment is compared case-insensitively and must be followed by '/'.
// cursor/ is the branch prefix Cursor cloud agents push.
var branchPrefixes = []struct {
	prefix string
	actor  string
}{
	{prefix: "cursor", actor: CloudAgent},
}

// agentLogins are git or GitHub identities that are agents by themselves.
// A person's login is not listed here.
var agentLogins = map[string]string{
	"cursoragent": CloudAgent,
	"cursor[bot]": CloudAgent,
}

var (
	coAuthorLine = regexp.MustCompile(`(?im)^[ \t]*co-authored-by:[ \t]*(.+?)[ \t]*$`)
	claudeName   = regexp.MustCompile(`(?i)^claude(?:\s+(?:opus|sonnet|haiku)(?:\s+[\w.]+)?)?$`)
	codexName    = regexp.MustCompile(`(?i)^(?:(?:openai\s+)?codex|gpt(?:[-.\s]?\d+(?:\.\d+)*)?[-.\s]?codex)$`)
	cursorName   = regexp.MustCompile(`(?i)^cursor(?:\s+agent)?$`)
	unlistedName = regexp.MustCompile(`(?i)^(copilot|github copilot|devin|devin ai|aider)$`)
)

type claim struct {
	actor    string
	unlisted bool
}

// Attribute selects one actor for the events of a work item.
//
// The first rule that finds evidence wins. A rule that finds two actors, or
// an agent it cannot name, returns Unknown and the later rules do not run.
//
//  1. Co-authored-by trailers, and an agent identity in the commit or pull
//     request actor field.
//  2. A linker session-to-commit link (a hard or soft reason, with both a
//     session and a commit in the item).
//  3. Agent sessions when the item has no commit. The session source names
//     the actor. A branch recorded on that session does not replace it.
//  4. An agent branch prefix, on an event branch or in a branch reason.
//  5. A session and a commit that the linker did not join, and no branch
//     prefix: Unknown.
//  6. A commit or pull request with no agent evidence: Human.
//  7. Anything else: Unknown.
func Attribute(events []Event) Result {
	if res, ok := fromIdentities(events); ok {
		return res
	}
	if res, ok := fromSessionCommits(events); ok {
		return res
	}
	if !hasKind(events, event.KindCommit) {
		if res, ok := fromSessions(events); ok {
			return res
		}
	}
	if res, ok := fromBranches(events); ok {
		return res
	}
	if hasKind(events, event.KindCommit) && hasKind(events, event.KindAgentSession) {
		return Result{Actor: Unknown, Evidence: EvidenceUnlinked}
	}
	if hasKind(events, event.KindCommit) || hasKind(events, event.KindPR) {
		return Result{Actor: Human, Evidence: EvidenceHuman}
	}
	return Result{Actor: Unknown, Evidence: EvidenceNone}
}

func fromIdentities(events []Event) (Result, bool) {
	var claims []claim
	for _, ev := range events {
		if ev.Kind != event.KindCommit && ev.Kind != event.KindPR {
			continue
		}
		claims = append(claims, scanTrailers(ev.Title)...)
		claims = append(claims, scanTrailers(ev.Summary)...)
		claims = append(claims, claimsFromActor(ev.Actor)...)
	}
	return decideClaims(claims, EvidenceCoAuthoredBy)
}

func fromSessionCommits(events []Event) (Result, bool) {
	if !hasKind(events, event.KindCommit) || !hasKind(events, event.KindAgentSession) || !hasLink(events) {
		return Result{}, false
	}
	return decideSessions(sessionsOf(events), EvidenceSessionCommit)
}

func fromBranches(events []Event) (Result, bool) {
	var actors []string
	for _, ev := range events {
		if id, ok := branchActor(ev.Branch); ok {
			actors = append(actors, id)
		}
		for _, part := range reasonParts(ev.Reason) {
			name, ok := reasonBranch(part)
			if !ok {
				continue
			}
			if id, ok := branchActor(name); ok {
				actors = append(actors, id)
			}
		}
	}
	uniq := uniqueActors(actors)
	if len(uniq) == 0 {
		return Result{}, false
	}
	if len(uniq) != 1 {
		return Result{Actor: Unknown, Evidence: EvidenceConflict}, true
	}
	return Result{Actor: uniq[0], Evidence: EvidenceBranch}, true
}

func fromSessions(events []Event) (Result, bool) {
	sessions := sessionsOf(events)
	if len(sessions) == 0 {
		return Result{}, false
	}
	return decideSessions(sessions, EvidenceSession)
}

func sessionsOf(events []Event) []Event {
	var out []Event
	for _, ev := range events {
		if ev.Kind == event.KindAgentSession {
			out = append(out, ev)
		}
	}
	return out
}

func decideSessions(sessions []Event, evidence string) (Result, bool) {
	var claims []claim
	for _, ev := range sessions {
		source := strings.ToLower(strings.TrimSpace(ev.Source))
		if id, ok := sessionSources[source]; ok {
			claims = append(claims, claim{actor: id})
			continue
		}
		claims = append(claims, claim{unlisted: true})
	}
	return decideClaims(claims, evidence)
}

func decideClaims(claims []claim, evidence string) (Result, bool) {
	if len(claims) == 0 {
		return Result{}, false
	}
	var actors []string
	unlisted := false
	for _, c := range claims {
		if c.unlisted || c.actor == "" {
			unlisted = true
			continue
		}
		actors = append(actors, c.actor)
	}
	uniq := uniqueActors(actors)
	if unlisted && len(uniq) == 0 {
		return Result{Actor: Unknown, Evidence: EvidenceUnrecognized}, true
	}
	if unlisted || len(uniq) != 1 {
		return Result{Actor: Unknown, Evidence: EvidenceConflict}, true
	}
	return Result{Actor: uniq[0], Evidence: evidence}, true
}

func uniqueActors(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, id := range in {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func hasKind(events []Event, kind event.Kind) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// hasLink reports a hard or soft linker reason. Pass-3 leftovers
// ("leftover:…", "session only") and override:unlink are not links.
func hasLink(events []Event) bool {
	for _, ev := range events {
		for _, part := range reasonParts(ev.Reason) {
			if linkReason(part) {
				return true
			}
		}
	}
	return false
}

func linkReason(part string) bool {
	switch {
	case strings.HasPrefix(part, "files overlap:"):
	case strings.HasPrefix(part, "head branch:"):
	case strings.HasPrefix(part, "only session"):
	case strings.HasPrefix(part, "git branch:"):
	case strings.HasPrefix(part, "revert:"):
	case strings.HasPrefix(part, "ref:"):
	case strings.HasPrefix(part, "sha:"):
	case strings.HasPrefix(part, "branch:"):
	case strings.HasPrefix(part, "lineage:"):
	case part == "override:link":
	default:
		return false
	}
	return true
}

func reasonParts(reason string) []string {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	raw := strings.Split(reason, ", ")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func reasonBranch(part string) (string, bool) {
	for _, prefix := range []string{"head branch:", "git branch:", "branch:"} {
		if !strings.HasPrefix(part, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(part, prefix))
		if name == "" {
			return "", false
		}
		return name, true
	}
	return "", false
}

func branchActor(branch string) (string, bool) {
	branch = normalizeBranch(branch)
	if !strings.Contains(branch, "/") {
		return "", false
	}
	first, _, _ := strings.Cut(branch, "/")
	first = strings.ToLower(first)
	for _, rule := range branchPrefixes {
		if first == rule.prefix {
			return rule.actor, true
		}
	}
	return "", false
}

func normalizeBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if rest, ok := strings.CutPrefix(branch, "refs/remotes/"); ok {
		if _, name, found := strings.Cut(rest, "/"); found {
			branch = name
		}
	}
	return branch
}

func scanTrailers(text string) []claim {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	matches := coAuthorLine.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var out []claim
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name, email := splitIdentity(match[1])
		out = append(out, claimsFromClassify(name, email)...)
	}
	return out
}

func claimsFromActor(raw string) []claim {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "@") || strings.Contains(raw, "<") {
		name, email := splitIdentity(raw)
		if email == "" && strings.Contains(raw, "@") && !strings.Contains(raw, " ") {
			email = raw
		}
		return claimsFromClassify(name, email)
	}
	if id, ok := agentLogins[strings.ToLower(raw)]; ok {
		return []claim{{actor: id}}
	}
	return nil
}

func claimsFromClassify(name, email string) []claim {
	kind, id := classifyIdentity(name, email)
	switch kind {
	case identActor:
		return []claim{{actor: id}}
	case identUnlisted:
		return []claim{{unlisted: true}}
	default:
		return nil
	}
}

type identKind int

const (
	identNone identKind = iota
	identActor
	identUnlisted
)

func classifyIdentity(name, email string) (identKind, string) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.Trim(strings.TrimSpace(email), "<>"))
	local, host := splitEmail(email)

	if host == "anthropic.com" && (local == "noreply" || strings.Contains(local, "claude") || claudeName.MatchString(name)) {
		return identActor, Claude
	}
	if email == "" && claudeName.MatchString(name) {
		return identActor, Claude
	}

	if host == "openai.com" && strings.Contains(local, "codex") {
		return identActor, Codex
	}
	if (email == "" || host == "openai.com") && codexName.MatchString(name) {
		return identActor, Codex
	}

	if email == "cursoragent@cursor.com" || (host == "cursor.com" && (local == "cursoragent" || local == "cursor")) {
		return identActor, CloudAgent
	}
	if (email == "" || host == "cursor.com" || host == "cursor.sh") && cursorName.MatchString(name) {
		return identActor, CloudAgent
	}

	if unlistedAgent(name, email, local, host) {
		return identUnlisted, ""
	}
	return identNone, ""
}

func unlistedAgent(name, email, local, host string) bool {
	if strings.Contains(email, "[bot]") {
		return true
	}
	if host == "github.com" && (local == "copilot" || strings.HasPrefix(local, "copilot+")) {
		return true
	}
	if host == "aider.chat" {
		return true
	}
	if email == "" && unlistedName.MatchString(name) {
		return true
	}
	if strings.HasSuffix(host, "noreply.github.com") && unlistedName.MatchString(name) {
		return true
	}
	return false
}

func splitIdentity(value string) (name, email string) {
	value = strings.TrimSpace(value)
	if i := strings.Index(value, ">"); i >= 0 {
		value = strings.TrimSpace(value[:i+1])
	}
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") && strings.Count(value, "<") == 1 {
		return "", strings.Trim(value, "<> \t")
	}
	namePart, emailPart, ok := strings.Cut(value, "<")
	if ok && strings.HasSuffix(strings.TrimSpace(emailPart), ">") {
		emailPart = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(emailPart), ">"))
		return strings.TrimSpace(namePart), emailPart
	}
	return strings.TrimSpace(value), ""
}

func splitEmail(email string) (local, host string) {
	local, host, ok := strings.Cut(email, "@")
	if !ok {
		return "", ""
	}
	return local, host
}
