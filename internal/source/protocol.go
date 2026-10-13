package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const (
	lineEvent  = "event"
	lineLog    = "log"
	lineCursor = "cursor"
)

// Limits keep a misbehaving plugin from exhausting memory.
var (
	maxMessageBytes = 1 << 20
	maxStderrBytes  = 8 << 10
	maxCursorBytes  = 64 << 10
	maxVersionRunes = 64
	// maxCollectEvents is how many events one collect may buffer.
	// Events are delivered only after exit 0, so this bounds memory.
	maxCollectEvents = 100000
)

// wireLine is one collect stdout message.
type wireLine struct {
	Type   string          `json:"type"`
	Event  json.RawMessage `json:"event,omitempty"`
	Level  string          `json:"level,omitempty"`
	Msg    string          `json:"msg,omitempty"`
	Cursor *string         `json:"cursor,omitempty"`
}

// EncodeManifest validates m and returns its JSON encoding plus a newline.
func EncodeManifest(m Manifest) ([]byte, error) {
	m = normalizeManifest(m)
	if err := validateManifest(m); err != nil {
		return nil, err
	}
	return encodeJSON(m)
}

// DecodeManifest parses a describe document. Pretty-printed JSON is accepted.
// Trailing non-whitespace data is rejected.
func DecodeManifest(data []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: invalid describe response", ErrProtocol)
	}
	if err := rejectTrailing(dec); err != nil {
		return Manifest{}, fmt.Errorf("%w: trailing data after describe response", ErrProtocol)
	}
	m = normalizeManifest(m)
	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// EncodeRequest validates req and returns its JSON encoding plus a newline.
// A zero Protocol is written as protocol v1. Nil config, emails, and handles
// are written as empty values so a plugin can index them.
func EncodeRequest(req CollectRequest) ([]byte, error) {
	req = fillRequest(req)
	if req.Protocol == 0 {
		req.Protocol = ProtocolVersion
	}
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	return encodeJSON(req)
}

// DecodeRequest parses a collect request.
func DecodeRequest(data []byte) (CollectRequest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return CollectRequest{}, fmt.Errorf("%w: empty collect request", ErrConfig)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var req CollectRequest
	if err := dec.Decode(&req); err != nil {
		return CollectRequest{}, fmt.Errorf("%w: invalid collect request", ErrConfig)
	}
	if err := rejectTrailing(dec); err != nil {
		return CollectRequest{}, fmt.Errorf("%w: trailing data after collect request", ErrConfig)
	}
	req = fillRequest(req)
	if err := validateRequest(req); err != nil {
		return CollectRequest{}, err
	}
	return req, nil
}

func normalizeManifest(m Manifest) Manifest {
	m.Name = strings.TrimSpace(m.Name)
	m.Version = strings.TrimSpace(m.Version)
	if m.Config == nil {
		m.Config = map[string]Field{}
	}
	return m
}

func validateManifest(m Manifest) error {
	if m.Protocol != ProtocolVersion {
		return protocolf("protocol %d", m.Protocol)
	}
	if !validName(m.Name) {
		return protocolf("invalid name")
	}
	if m.Version == "" || utf8.RuneCountInString(m.Version) > maxVersionRunes || !printable(m.Version) {
		return protocolf("invalid version")
	}
	for key, field := range m.Config {
		if !validName(key) {
			return protocolf("invalid config field")
		}
		switch field.Type {
		case TypeString, TypeStrings, TypeSecret:
		default:
			return protocolf("invalid config type for %s", key)
		}
	}
	return nil
}

func fillRequest(req CollectRequest) CollectRequest {
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	if req.Identity.Emails == nil {
		req.Identity.Emails = []string{}
	}
	if req.Identity.Handles == nil {
		req.Identity.Handles = map[string]string{}
	}
	return req
}

func validateRequest(req CollectRequest) error {
	if req.Protocol != ProtocolVersion {
		return configf("protocol %d", req.Protocol)
	}
	if req.Since.IsZero() || req.Until.IsZero() {
		return configf("since and until are required")
	}
	if !req.Until.After(req.Since) {
		return configf("until must be after since")
	}
	if req.Cursor != nil && len(*req.Cursor) > maxCursorBytes {
		return configf("cursor exceeds %d bytes", maxCursorBytes)
	}
	return nil
}

// acceptEvent validates a plugin event and assigns name when source is empty.
// A non-empty source that differs from name is rejected.
func acceptEvent(name string, raw json.RawMessage) (event.Event, error) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return event.Event{}, fmt.Errorf("%w: event is empty", ErrProtocol)
	}
	var ev event.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return event.Event{}, fmt.Errorf("%w: %s", ErrProtocol, oneLine(err.Error()))
	}
	if strings.TrimSpace(ev.Source) == "" {
		ev.Source = name
	}
	prepared, err := event.Prepare(ev)
	if err != nil {
		return event.Event{}, fmt.Errorf("%w: %s", ErrProtocol, oneLine(err.Error()))
	}
	if prepared.Source != name {
		return event.Event{}, fmt.Errorf("%w: event source %s", ErrProtocol, oneLine(prepared.Source))
	}
	return prepared, nil
}

func acceptLog(level, msg string) (LogLine, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "warning" {
		level = "warn"
	}
	switch level {
	case "info", "warn", "error":
	default:
		return LogLine{}, fmt.Errorf("%w: invalid log level", ErrProtocol)
	}
	msg = strings.TrimSpace(msg)
	if msg == "" || !printable(msg) {
		return LogLine{}, fmt.Errorf("%w: invalid log message", ErrProtocol)
	}
	return LogLine{Level: level, Msg: msg}, nil
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeLine(line wireLine) ([]byte, error) {
	return encodeJSON(line)
}

func rejectTrailing(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("trailing data")
	}
	return err
}

func protocolf(format string, args ...any) error {
	return &Failure{Reason: fmt.Sprintf(format, args...), Err: ErrProtocol}
}

func configf(format string, args ...any) error {
	return &Failure{Reason: fmt.Sprintf(format, args...), Err: ErrConfig}
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			return false
		}
	}
	return true
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
