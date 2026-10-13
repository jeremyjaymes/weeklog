package event

import "strings"

// MaxSummaryRunes is the stored length of Summary. Longer text is truncated
// by Prepare.
const MaxSummaryRunes = 500

// Kind is the event category. Known kinds are canonical lowercase values.
// Any other non-empty kind is stored and rendered later under "Other".
type Kind string

const (
	KindCommit           Kind = "commit"
	KindAgentSession     Kind = "agent_session"
	KindPR               Kind = "pr"
	KindPRReview         Kind = "pr_review"
	KindIssue            Kind = "issue"
	KindTicket           Kind = "ticket"
	KindTicketTransition Kind = "ticket_transition"
	KindComment          Kind = "comment"
)

// Known reports whether k is one of the v1 kinds. Comparison is case-insensitive.
func (k Kind) Known() bool {
	switch Kind(strings.ToLower(strings.TrimSpace(string(k)))) {
	case KindCommit, KindAgentSession, KindPR, KindPRReview, KindIssue, KindTicket, KindTicketTransition, KindComment:
		return true
	default:
		return false
	}
}

// State is the optional workflow state of an event.
type State string

const (
	StateOpen      State = "open"
	StateClosed    State = "closed"
	StateMerged    State = "merged"
	StateDone      State = "done"
	StateAbandoned State = "abandoned"
)

// Known reports whether s is one of the v1 states. Comparison is case-insensitive.
// The empty state is unset, not unknown; Known returns false for it.
func (s State) Known() bool {
	switch State(strings.ToLower(strings.TrimSpace(string(s)))) {
	case StateOpen, StateClosed, StateMerged, StateDone, StateAbandoned:
		return true
	default:
		return false
	}
}
