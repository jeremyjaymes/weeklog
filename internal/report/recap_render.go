package report

import (
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const (
	// DefaultRecapWidth is the lipgloss content width, including the one-cell
	// padding on each side. The border sits outside it.
	DefaultRecapWidth = 72

	colorBorder = "#4B5563"
	colorTitle  = "#F3F4F6"
	colorMuted  = "#9CA3AF"
	colorAccent = "#7DD3FC"
	colorDay    = "#38BDF8"
	colorEmpty  = "#374151"
	colorDelta  = "#FBBF24"
	colorGraph  = "#C4B5FD"
	colorSoft   = "#FDBA74"

	recapBarWidth  = 18
	recapLaneLabel = 12
	maxSparkDays   = 21
	maxLabeledDays = 14
)

// sparkBlocks are the eight vertical levels of a sparkline, shortest first.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// RenderOptions selects the recap's width and whether lanes are colored.
// Color false is the same box with no ANSI. Callers that must keep the
// Markdown report (a pipe, NO_COLOR, or --plain) render that report instead.
type RenderOptions struct {
	// Width is the content width passed to lipgloss. Values below 48 use
	// DefaultRecapWidth.
	Width int
	// Color paints source lanes, bars, and the border. False leaves the
	// unicode box unstyled so a golden file can lock the layout.
	Color bool
}

// RenderRecap draws recap as a bordered terminal summary.
func RenderRecap(recap Recap, opt RenderOptions) string {
	width := opt.Width
	if width < 48 {
		width = DefaultRecapWidth
	}
	if width > 120 {
		width = 120
	}
	ren := lipgloss.NewRenderer(io.Discard)
	if opt.Color {
		ren.SetColorProfile(termenv.TrueColor)
	} else {
		ren.SetColorProfile(termenv.Ascii)
	}
	ren.SetHasDarkBackground(true)

	content := width - 2
	lines := layoutRecap(recap, content)
	painted := make([]string, len(lines))
	for i, line := range lines {
		painted[i] = paintLine(ren, line)
	}
	box := ren.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Padding(0, 1).
		Width(width)
	return box.Render(strings.Join(painted, "\n")) + "\n"
}

// span is one painted run. layoutRecap builds these. The renderer colors
// them, and the plain profile prints Text alone.
type span struct {
	text  string
	color string
	bold  bool
	faint bool
}

func paintLine(ren *lipgloss.Renderer, line []span) string {
	if len(line) == 0 {
		return ""
	}
	parts := make([]string, 0, len(line))
	for _, sp := range line {
		if sp.text == "" {
			continue
		}
		style := ren.NewStyle()
		if sp.color != "" {
			style = style.Foreground(lipgloss.Color(sp.color))
		}
		if sp.bold {
			style = style.Bold(true)
		}
		if sp.faint {
			style = style.Faint(true)
		}
		parts = append(parts, style.Render(sp.text))
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// layoutRecap is the section order. Delta sits under the summary. Graph sits
// under the source lanes. A nil panel adds no rows.
func layoutRecap(recap Recap, width int) [][]span {
	if width < 24 {
		width = 24
	}
	counts, detail := summaryParts(recap.Summary, time.Duration(recap.Summary.AgentSeconds)*time.Second)
	lines := [][]span{
		{{text: truncate(recap.Heading, width), bold: true, color: colorTitle}},
		{{text: truncate(counts, width), color: colorTitle}},
		{{text: truncate(detail, width), color: colorMuted, faint: true}},
	}
	if recap.Delta != nil {
		if text := strings.TrimSpace(recap.Delta.Line); text != "" {
			lines = append(lines, []span{{text: truncate(text, width), color: colorDelta}})
		}
	}
	lines = append(lines, nil)
	lines = append(lines, daySection(recap.Days, width)...)
	lines = append(lines, nil)
	lines = append(lines, laneSection(recap.Lanes, width)...)
	if graphReady(recap.Graph) {
		lines = append(lines, nil)
		lines = append(lines, graphSection(recap.Graph, width)...)
	}
	lines = append(lines, nil)
	lines = append(lines, repoSection(recap.Repos, width)...)
	lines = append(lines, nil)
	lines = append(lines, prSection(recap.PRs, width)...)
	return lines
}

func daySection(days []DayStat, width int) [][]span {
	lines := [][]span{titleSpan("Days")}
	if len(days) == 0 {
		lines = append(lines, muted("No days in this window", width))
		return lines
	}
	if len(days) > maxLabeledDays {
		counts := make([]int, len(days))
		for i, day := range days {
			counts[i] = day.Count
		}
		label := days[0].Date.Format("Jan 2") + " – " + days[len(days)-1].Date.Format("Jan 2")
		lines = append(lines, sparkRow(label, counts, width, colorDay))
		return lines
	}
	max := 1
	for _, day := range days {
		if day.Count > max {
			max = day.Count
		}
	}
	barWidth := recapBarWidth
	room := width - 4 - 4 // "Mon " and " 123"
	if room < barWidth {
		barWidth = room
	}
	if barWidth < 4 {
		barWidth = 4
	}
	for _, day := range days {
		filled, empty := barCells(day.Count, max, barWidth)
		label := padRight(day.Label, 3) + " "
		count := " " + padLeft(strconv.Itoa(day.Count), 3)
		lines = append(lines, []span{
			{text: label, color: colorMuted},
			{text: strings.Repeat("█", filled), color: colorDay},
			{text: strings.Repeat("░", empty), color: colorEmpty},
			{text: count, color: colorMuted},
		})
	}
	return lines
}

func laneSection(lanes []Lane, width int) [][]span {
	lines := [][]span{titleSpan("Sources")}
	if len(lanes) == 0 {
		lines = append(lines, muted("No source activity", width))
		return lines
	}
	max := 1
	for _, lane := range lanes {
		for _, n := range lane.Counts {
			if n > max {
				max = n
			}
		}
	}
	for _, lane := range lanes {
		counts := lane.Counts
		if len(counts) > maxSparkDays {
			counts = bucketCounts(counts, maxSparkDays)
		}
		spark := sparkline(counts, max, len(counts) <= 10)
		label := padRight(lane.Label, recapLaneLabel) + " "
		count := " " + padLeft(strconv.Itoa(lane.Total), 3)
		room := width - utf8.RuneCountInString(count)
		body := label + spark
		if utf8.RuneCountInString(body) > room && room > utf8.RuneCountInString(label) {
			spark = truncate(spark, room-utf8.RuneCountInString(label))
		}
		color := lane.Color
		if color == "" {
			color = laneColor(lane.Source)
		}
		lines = append(lines, []span{
			{text: label, color: color, bold: true},
			{text: spark, color: color},
			{text: count, color: colorMuted},
		})
	}
	return lines
}

func graphSection(g *Graph, width int) [][]span {
	title := "Chain"
	if g != nil && strings.TrimSpace(g.Title) != "" {
		title = strings.TrimSpace(g.Title)
	}
	lines := [][]span{titleSpan(title)}
	if g != nil && len(g.Chains) > 0 {
		for i, chain := range g.Chains {
			if i > 0 {
				lines = append(lines, nil)
			}
			lines = append(lines, renderChain(chain, width)...)
		}
		if g.More > 0 {
			lines = append(lines, graphMoreSpan(g.More, width))
		}
		return lines
	}
	for _, row := range graphLines(g) {
		lines = append(lines, []span{{text: truncate(row, width), color: colorGraph}})
	}
	return lines
}

func graphReady(g *Graph) bool {
	if g == nil {
		return false
	}
	if len(g.Chains) > 0 {
		return true
	}
	return graphLines(g) != nil
}

func graphLines(g *Graph) []string {
	if g == nil {
		return nil
	}
	var rows []string
	for _, row := range g.Lines {
		row = strings.TrimRight(row, " \t")
		if row == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// renderChain draws one work item in git-log order: session, then commits,
// then the pull request or issue. A hard link uses * and |. A soft link uses
// o and :. The stored reason sits on the following line, dimmed.
func renderChain(chain GraphChain, width int) [][]span {
	var lines [][]span
	headConnect := len(chain.Tail) > 0 || chain.More > 0
	for i, node := range chain.Head {
		connect := i < len(chain.Head)-1 || headConnect
		lines = append(lines, renderGraphNode(node, width, connect)...)
	}
	if chain.More > 0 {
		lines = append(lines, renderCollapse(chain, width))
	}
	for i, node := range chain.Tail {
		connect := i < len(chain.Tail)-1
		lines = append(lines, renderGraphNode(node, width, connect)...)
	}
	return lines
}

func renderGraphNode(node GraphNode, width int, connect bool) [][]span {
	glyph, edge, color := "*", "|", colorGraph
	if node.Soft {
		glyph, edge, color = "o", ":", colorSoft
	}
	lines := [][]span{{
		{text: graphHeadText(glyph, node.Label, node.Detail, width), color: color, bold: true},
	}}
	reason := strings.TrimSpace(node.Reason)
	if reason == "" {
		if connect {
			lines = append(lines, []span{{text: truncate(edge, width), color: color}})
		}
		return lines
	}
	prefix := "  "
	if connect {
		prefix = edge + " "
	}
	if utf8.RuneCountInString(prefix) >= width {
		return append(lines, []span{{text: truncate(prefix, width), color: color}})
	}
	shown := truncate(reason, width-utf8.RuneCountInString(prefix))
	return append(lines, []span{
		{text: prefix, color: color},
		{text: shown, color: colorMuted, faint: true},
	})
}

func graphHeadText(glyph, label, detail string, width int) string {
	if width <= 0 {
		return ""
	}
	mark := glyph + " "
	if utf8.RuneCountInString(mark) > width {
		return truncate(mark, width)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "event"
	}
	rest := width - utf8.RuneCountInString(mark)
	detail = strings.TrimSpace(detail)
	if detail == "" || rest <= utf8.RuneCountInString(label) {
		return mark + truncate(label, rest)
	}
	const sep = " · "
	room := rest - utf8.RuneCountInString(label) - utf8.RuneCountInString(sep)
	if room < 4 {
		return mark + truncate(label, rest)
	}
	return mark + label + sep + truncate(detail, room)
}

func renderCollapse(chain GraphChain, width int) []span {
	edge, color := "|", colorGraph
	if len(chain.Tail) > 0 && chain.Tail[0].Soft {
		edge, color = ":", colorSoft
	} else if len(chain.Tail) == 0 && len(chain.Head) > 0 && chain.Head[len(chain.Head)-1].Soft {
		edge, color = ":", colorSoft
	}
	return graphEdgeMore(edge, color, chain.More, width)
}

func graphMoreSpan(n, width int) []span {
	return []span{{text: truncate(moreLabel(n), width), color: colorMuted, faint: true}}
}

func graphEdgeMore(edge, color string, n, width int) []span {
	prefix := edge + " "
	label := moreLabel(n)
	if utf8.RuneCountInString(prefix) >= width {
		return []span{{text: truncate(prefix+label, width), color: colorMuted, faint: true}}
	}
	room := width - utf8.RuneCountInString(prefix)
	return []span{
		{text: prefix, color: color},
		{text: truncate(label, room), color: colorMuted, faint: true},
	}
}

func moreLabel(n int) string {
	if n < 1 {
		n = 1
	}
	return "+" + strconv.Itoa(n) + " more"
}

func repoSection(repos []RepoStat, width int) [][]span {
	lines := [][]span{titleSpan("Top repos")}
	if len(repos) == 0 {
		lines = append(lines, muted("No repositories", width))
		return lines
	}
	for _, repo := range repos {
		stats := repoStats(repo)
		lines = append(lines, []span{{text: fitLeft(repo.Name, stats, width), color: colorTitle}})
	}
	return lines
}

func prSection(prs []PRStat, width int) [][]span {
	lines := [][]span{titleSpan("Top PRs")}
	if len(prs) == 0 {
		lines = append(lines, muted("No pull requests", width))
		return lines
	}
	for _, pr := range prs {
		lines = append(lines, colorPR(prLeft(pr), prRight(pr), width, pr.State))
	}
	return lines
}

func colorPR(left, right string, width int, state string) []span {
	if right == "" {
		return []span{{text: truncate(left, width), bold: true, color: colorTitle}}
	}
	room := width - utf8.RuneCountInString(right) - 2
	if room < 4 {
		return []span{{text: truncate(strings.TrimSpace(left+" "+right), width), color: colorTitle}}
	}
	shown := left
	if utf8.RuneCountInString(shown) > room {
		shown = truncate(shown, room)
	}
	pad := width - utf8.RuneCountInString(shown) - utf8.RuneCountInString(right)
	if pad < 2 {
		pad = 2
	}
	return []span{
		{text: shown, bold: true, color: colorTitle},
		{text: strings.Repeat(" ", pad)},
		{text: right, color: stateColor(state)},
	}
}

func prLeft(pr PRStat) string {
	var b strings.Builder
	if pr.Number != "" {
		b.WriteByte('#')
		b.WriteString(pr.Number)
		b.WriteByte(' ')
	}
	title := pr.Title
	if title == "" {
		title = "(untitled)"
	}
	b.WriteString(title)
	return b.String()
}

func prRight(pr PRStat) string {
	var parts []string
	if pr.State != "" {
		parts = append(parts, pr.State)
	}
	if pr.Repo != "" {
		parts = append(parts, pr.Repo)
	}
	return strings.Join(parts, " · ")
}

func repoStats(repo RepoStat) string {
	var parts []string
	if repo.Commits > 0 {
		parts = append(parts, plural(repo.Commits, "commit", "commits"))
	}
	if repo.PRs > 0 {
		parts = append(parts, plural(repo.PRs, "PR", "PRs"))
	}
	if repo.Sessions > 0 {
		parts = append(parts, plural(repo.Sessions, "session", "sessions"))
	}
	if len(parts) == 0 {
		return plural(repo.Other, "event", "events")
	}
	return strings.Join(parts, " · ")
}

func titleSpan(text string) []span {
	return []span{{text: text, bold: true, color: colorAccent}}
}

func muted(text string, width int) []span {
	return []span{{text: truncate(text, width), color: colorMuted, faint: true}}
}

func sparkRow(label string, counts []int, width int, color string) []span {
	if len(counts) > maxSparkDays {
		counts = bucketCounts(counts, maxSparkDays)
	}
	max := 1
	for _, n := range counts {
		if n > max {
			max = n
		}
	}
	spark := sparkline(counts, max, false)
	prefix := padRight(label, 16) + " "
	count := " " + strconv.Itoa(sumInts(counts))
	room := width - utf8.RuneCountInString(count)
	if utf8.RuneCountInString(prefix+spark) > room && room > utf8.RuneCountInString(prefix) {
		spark = truncate(spark, room-utf8.RuneCountInString(prefix))
	}
	return []span{
		{text: prefix, color: colorMuted},
		{text: spark, color: color},
		{text: count, color: colorMuted},
	}
}

func barCells(n, max, width int) (filled, empty int) {
	if width < 1 {
		return 0, 0
	}
	if n <= 0 || max <= 0 {
		return 0, width
	}
	filled = n * width / max
	if filled < 1 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return filled, width - filled
}

func sparkline(counts []int, max int, spaced bool) string {
	if max < 1 {
		max = 1
	}
	var b strings.Builder
	for i, n := range counts {
		if i > 0 && spaced {
			b.WriteByte(' ')
		}
		b.WriteRune(sparkRune(n, max))
	}
	return b.String()
}

func sparkRune(n, max int) rune {
	if n <= 0 {
		return '·'
	}
	if max < 1 {
		max = 1
	}
	last := len(sparkBlocks) - 1
	idx := n * last / max
	if idx < 0 {
		idx = 0
	}
	if idx > last {
		idx = last
	}
	return sparkBlocks[idx]
}

func bucketCounts(counts []int, buckets int) []int {
	if buckets < 1 || len(counts) <= buckets {
		return counts
	}
	out := make([]int, buckets)
	for i, n := range counts {
		idx := i * buckets / len(counts)
		if idx >= buckets {
			idx = buckets - 1
		}
		out[idx] += n
	}
	return out
}

func fitLeft(left, right string, width int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" {
		return truncate(left, width)
	}
	if width <= 0 {
		return left + " " + right
	}
	room := width - utf8.RuneCountInString(right) - 2
	if room < 4 {
		return truncate(left+" "+right, width)
	}
	if utf8.RuneCountInString(left) > room {
		left = truncate(left, room)
	}
	pad := width - utf8.RuneCountInString(left) - utf8.RuneCountInString(right)
	if pad < 2 {
		pad = 2
	}
	return left + strings.Repeat(" ", pad) + right
}

func stateColor(state string) string {
	switch event.State(state) {
	case event.StateMerged:
		return "#C084FC"
	case event.StateOpen:
		return "#34D399"
	case event.StateClosed, event.StateAbandoned:
		return "#F87171"
	case event.StateDone:
		return "#60A5FA"
	default:
		return colorMuted
	}
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

func padRight(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return truncate(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}

func padLeft(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}

func sumInts(ns []int) int {
	total := 0
	for _, n := range ns {
		total += n
	}
	return total
}
