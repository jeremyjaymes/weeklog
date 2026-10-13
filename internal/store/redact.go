package store

import "github.com/jeremyjaymes/weeklog/internal/redact"

// SetRedactor installs the redactor used before events are written.
// A nil redactor restores the built-in rules. Redaction stays on.
func (s *Store) SetRedactor(r *redact.Redactor) {
	if s == nil {
		return
	}
	if r == nil {
		r = redact.Default()
	}
	s.redactor.Store(r)
}

func (s *Store) activeRedactor() *redact.Redactor {
	if s == nil {
		return redact.Default()
	}
	if r := s.redactor.Load(); r != nil {
		return r
	}
	return redact.Default()
}
