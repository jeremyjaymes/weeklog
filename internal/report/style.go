package report

import (
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Style is the lipgloss palette shared by the recap and the interactive
// week browser. Color false is the plain profile: the same marks and the
// same border, with no ANSI.
type Style struct {
	ren *lipgloss.Renderer
}

// NewStyle builds the renderer RenderRecap uses.
func NewStyle(color bool) Style {
	ren := lipgloss.NewRenderer(io.Discard)
	if color {
		ren.SetColorProfile(termenv.TrueColor)
	} else {
		ren.SetColorProfile(termenv.Ascii)
	}
	ren.SetHasDarkBackground(true)
	return Style{ren: ren}
}

// Tone selects a recap color.
type Tone int

const (
	// ToneTitle is the bold heading color.
	ToneTitle Tone = iota
	// ToneText is the heading color without bold.
	ToneText
	// ToneMuted is the dim secondary color.
	ToneMuted
	// ToneAccent is the section heading color.
	ToneAccent
	// ToneDay is the day-bar color.
	ToneDay
	// ToneHard is the hard-link mark, the same violet as the session chain.
	ToneHard
	// ToneSoft is the soft-link mark.
	ToneSoft
	// ToneReason is the dimmed linker reason.
	ToneReason
	// ToneStatus colors a work-item status with the recap state colors.
	ToneStatus
)

// Segment is one painted run on a browser line.
type Segment struct {
	Text   string
	Tone   Tone
	Status string
	Bold   bool
}

// Line paints parts with the recap palette.
func (s Style) Line(parts ...Segment) string {
	if s.ren == nil {
		s = NewStyle(false)
	}
	spans := make([]span, 0, len(parts))
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		color, bold, faint := toneOf(part)
		spans = append(spans, span{text: part.Text, color: color, bold: bold, faint: faint})
	}
	return paintLine(s.ren, spans)
}

// Truncate shortens text the same way the recap does.
func (s Style) Truncate(text string, n int) string {
	return truncate(text, n)
}

// Box draws content in the recap's rounded border.
// width is the lipgloss content width, including the one-cell padding on
// each side. Values below 48 use DefaultRecapWidth.
func (s Style) Box(content string, width int) string {
	if s.ren == nil {
		s = NewStyle(false)
	}
	if width < 48 {
		width = DefaultRecapWidth
	}
	if width > 120 {
		width = 120
	}
	return s.frame(content, width)
}

// frame is the bordered box RenderRecap and the browser both draw.
func (s Style) frame(content string, width int) string {
	return s.ren.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Padding(0, 1).
		Width(width).
		Render(content)
}

func toneOf(part Segment) (color string, bold, faint bool) {
	switch part.Tone {
	case ToneTitle:
		return colorTitle, true, false
	case ToneText:
		return colorTitle, part.Bold, false
	case ToneMuted:
		return colorMuted, part.Bold, true
	case ToneAccent:
		return colorAccent, true, false
	case ToneDay:
		return colorDay, part.Bold, false
	case ToneHard:
		return colorGraph, true, false
	case ToneSoft:
		return colorSoft, true, false
	case ToneReason:
		return colorMuted, false, true
	case ToneStatus:
		return statusTone(part.Status), part.Bold, false
	default:
		return colorTitle, part.Bold, false
	}
}

func statusTone(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case statusShipped:
		return stateColor(string(event.StateMerged))
	case "in progress", statusInProgress:
		return stateColor(string(event.StateOpen))
	case "abandoned / stale", statusAbandoned:
		return stateColor(string(event.StateAbandoned))
	case statusExplored:
		return colorAccent
	default:
		return colorMuted
	}
}
