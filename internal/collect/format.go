package collect

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/redact"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

func writeOutcome(b *strings.Builder, out outcome) {
	for _, line := range out.logs {
		level := line.Level
		if level == "" {
			level = "info"
		}
		fmt.Fprintf(b, "%s: %s: %s\n", out.label, level, line.Msg)
	}
	switch out.status {
	case statusOK:
		fmt.Fprintf(b, "%s: ok (%s)\n", out.label, eventDetail(out.events, out.dropped))
	case statusSkipped:
		fmt.Fprintf(b, "%s: skipped (%s)\n", out.label, out.detail)
	case statusCanceled:
		fmt.Fprintf(b, "%s: canceled\n", out.label)
	default:
		fmt.Fprintf(b, "%s: failed (%s)\n", out.label, out.detail)
	}
}

func eventDetail(events, dropped int) string {
	detail := counted(events, "event", "events")
	if dropped > 0 {
		detail += fmt.Sprintf(", %d dropped", dropped)
	}
	return detail
}

func countsLine(events, failed, skipped int, canceled bool) string {
	parts := []string{counted(events, "event", "events")}
	if failed > 0 {
		parts = append(parts, counted(failed, "source failed", "sources failed"))
	}
	if skipped > 0 {
		parts = append(parts, counted(skipped, "source skipped", "sources skipped"))
	}
	if canceled {
		parts = append(parts, "canceled")
	}
	return strings.Join(parts, ", ")
}

func overallStatus(okN, failN int, linkErr error, aborted bool) string {
	switch {
	case aborted:
		return statusCanceled
	case failN > 0 && okN == 0:
		return statusFailed
	case failN > 0 || linkErr != nil:
		return statusPartial
	default:
		return statusOK
	}
}

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func redactLogs(r *redact.Redactor, logs []source.LogLine) []source.LogLine {
	if len(logs) == 0 {
		return nil
	}
	out := make([]source.LogLine, len(logs))
	for i, line := range logs {
		out[i] = source.LogLine{Level: line.Level, Msg: publicText(r, line.Msg)}
	}
	return out
}

func publicText(r *redact.Redactor, s string) string {
	if r == nil {
		r = redact.Default()
	}
	return truncate(oneLine(r.Text(s)), 300)
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}
