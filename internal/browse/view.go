package browse

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/report"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	width := m.width
	if width < 48 {
		width = report.DefaultRecapWidth
	}
	if width > 100 {
		width = 100
	}
	return m.style.Box(m.body(), width)
}

func (m Model) body() string {
	rows := m.rowLines()
	for len(rows) < m.page() {
		rows = append(rows, "")
	}
	lines := []string{m.headingLine(), m.sectionLine(), ""}
	lines = append(lines, rows...)
	lines = append(lines, "", m.footerLine())
	return strings.Join(lines, "\n")
}

func (m Model) headingLine() string {
	switch m.screen {
	case screenItems:
		if day, ok := m.currentDay(); ok {
			return m.plain(day.Label+"  "+day.When, report.ToneDay, true)
		}
	case screenLinks:
		if item, ok := m.currentItem(); ok {
			return m.plain(item.Title, report.ToneTitle, false)
		}
	}
	text := m.browser.Heading
	if strings.TrimSpace(text) == "" {
		text = "Week"
	}
	return m.plain(text, report.ToneTitle, false)
}

func (m Model) sectionLine() string {
	switch m.screen {
	case screenItems:
		return m.spread("Items", "", report.ToneAccent, report.ToneMuted, "")
	case screenLinks:
		right := ""
		status := ""
		if item, ok := m.currentItem(); ok {
			status = item.Status
			right = joinMeta(item.Status, item.Actor, item.Repo)
		}
		return m.spread("Linked", right, report.ToneAccent, report.ToneStatus, status)
	default:
		return m.plain("Days", report.ToneAccent, false)
	}
}

func (m Model) rowLines() []string {
	n := m.count()
	if n == 0 {
		return []string{m.plain(m.emptyText(), report.ToneMuted, false)}
	}
	off := m.offset[m.screen]
	if off < 0 {
		off = 0
	}
	end := off + m.page()
	if end > n {
		end = n
	}
	rows := make([]string, 0, end-off)
	for i := off; i < end; i++ {
		rows = append(rows, m.row(i))
	}
	return rows
}

func (m Model) row(index int) string {
	selected := index == m.cursor[m.screen]
	switch m.screen {
	case screenDays:
		return m.dayRow(m.browser.Days[index], selected)
	case screenItems:
		day, _ := m.currentDay()
		return m.itemRow(day.Items[index], selected)
	default:
		item, _ := m.currentItem()
		return m.linkRow(item.Links[index], selected)
	}
}

func (m Model) dayRow(day report.BrowserDay, selected bool) string {
	n := len(day.Items)
	right := "—"
	if n > 0 {
		right = countLabel(n, "item", "items")
	}
	return m.cursorLine(selected, day.Label+"  "+day.When, report.ToneDay, true, right, report.ToneMuted, "")
}

func (m Model) itemRow(item report.BrowserItem, selected bool) string {
	n := len(item.Links)
	links := "not linked"
	tone := report.ToneMuted
	if n > 0 {
		links = countLabel(n, "link", "links")
		tone = report.ToneStatus
	}
	right := joinMeta(item.Status, item.Actor, item.Repo, links)
	return m.cursorLine(selected, item.Title, report.ToneText, selected, right, tone, item.Status)
}

func (m Model) linkRow(link report.BrowserLink, selected bool) string {
	mark := "* "
	tone := report.ToneHard
	if link.Soft {
		mark = "o "
		tone = report.ToneSoft
	}
	body := strings.TrimSpace(link.Label)
	if detail := strings.TrimSpace(link.Detail); detail != "" {
		if body != "" {
			body += " · "
		}
		body += detail
	}
	if body == "" {
		body = "event"
	}
	reason := strings.TrimSpace(link.Reason)
	width := m.lineWidth()
	prefix, pTone := cursorPrefix(selected)
	room := width - utf8.RuneCountInString(prefix)
	if room < 1 {
		return m.style.Line(report.Segment{Text: m.style.Truncate(prefix, width), Tone: pTone})
	}
	markWidth := utf8.RuneCountInString(mark)
	if markWidth >= room {
		return m.style.Line(
			report.Segment{Text: prefix, Tone: pTone},
			report.Segment{Text: m.style.Truncate(mark, room), Tone: tone},
		)
	}
	rest := room - markWidth
	if reason == "" {
		return m.style.Line(
			report.Segment{Text: prefix, Tone: pTone},
			report.Segment{Text: mark + m.style.Truncate(body, rest), Tone: tone},
		)
	}
	const sep = "  "
	sepWidth := utf8.RuneCountInString(sep)
	bodyWidth := utf8.RuneCountInString(body)
	if rest <= bodyWidth+sepWidth+3 {
		return m.style.Line(
			report.Segment{Text: prefix, Tone: pTone},
			report.Segment{Text: mark + m.style.Truncate(body, rest), Tone: tone},
		)
	}
	reasonRoom := rest - bodyWidth - sepWidth
	return m.style.Line(
		report.Segment{Text: prefix, Tone: pTone},
		report.Segment{Text: mark + body + sep, Tone: tone},
		report.Segment{Text: m.style.Truncate(reason, reasonRoom), Tone: report.ToneReason},
	)
}

func (m Model) emptyText() string {
	switch m.screen {
	case screenItems:
		return "No items this day"
	case screenLinks:
		return "No linker-linked commits, pull requests, or sessions"
	default:
		return "No days in this window"
	}
}

func (m Model) footerLine() string {
	help := "j/k move · enter open · q quit"
	switch m.screen {
	case screenItems:
		help = "j/k move · enter open · esc back · q quit"
	case screenLinks:
		help = "j/k move · esc back · q quit"
	}
	n := m.count()
	if n == 0 {
		return m.plain(help, report.ToneMuted, false)
	}
	pos := strconv.Itoa(m.cursor[m.screen]+1) + "/" + strconv.Itoa(n)
	return m.spread(help, pos, report.ToneMuted, report.ToneMuted, "")
}

func (m Model) plain(text string, tone report.Tone, bold bool) string {
	return m.style.Line(report.Segment{
		Text: m.style.Truncate(text, m.lineWidth()),
		Tone: tone,
		Bold: bold,
	})
}

func (m Model) spread(left, right string, leftTone, rightTone report.Tone, status string) string {
	return m.cursorLine(false, left, leftTone, false, right, rightTone, status)
}

func (m Model) cursorLine(selected bool, left string, leftTone report.Tone, leftBold bool, right string, rightTone report.Tone, status string) string {
	width := m.lineWidth()
	prefix, pTone := cursorPrefix(selected)
	room := width - utf8.RuneCountInString(prefix)
	if room < 1 {
		return m.style.Line(report.Segment{Text: m.style.Truncate(prefix, width), Tone: pTone})
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" {
		return m.style.Line(
			report.Segment{Text: prefix, Tone: pTone},
			report.Segment{Text: m.style.Truncate(left, room), Tone: leftTone, Bold: leftBold},
		)
	}
	rightWidth := utf8.RuneCountInString(right)
	if rightWidth+2 >= room {
		return m.style.Line(
			report.Segment{Text: prefix, Tone: pTone},
			report.Segment{Text: m.style.Truncate(strings.TrimSpace(left+" "+right), room), Tone: leftTone, Bold: leftBold},
		)
	}
	leftRoom := room - rightWidth - 2
	shown := left
	if utf8.RuneCountInString(shown) > leftRoom {
		shown = m.style.Truncate(shown, leftRoom)
	}
	pad := room - utf8.RuneCountInString(shown) - rightWidth
	if pad < 2 {
		pad = 2
	}
	return m.style.Line(
		report.Segment{Text: prefix, Tone: pTone},
		report.Segment{Text: shown, Tone: leftTone, Bold: leftBold},
		report.Segment{Text: strings.Repeat(" ", pad)},
		report.Segment{Text: right, Tone: rightTone, Status: status},
	)
}

func (m Model) lineWidth() int {
	width := m.width
	if width < 48 {
		width = report.DefaultRecapWidth
	}
	if width > 100 {
		width = 100
	}
	n := width - 2
	if n < 1 {
		return 1
	}
	return n
}

func cursorPrefix(selected bool) (string, report.Tone) {
	if selected {
		return "> ", report.ToneAccent
	}
	return "  ", report.ToneMuted
}

func joinMeta(parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, " · ")
}

func countLabel(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}
