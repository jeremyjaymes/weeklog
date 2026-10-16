package browse

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeremyjaymes/weeklog/internal/report"
)

// screen is the drill-down level: day, then item, then linker links.
type screen int

const (
	screenDays screen = iota
	screenItems
	screenLinks
)

const (
	viewHeader = 3
	viewFooter = 2
	viewBorder = 2
)

// Options selects the first frame. Width is the lipgloss content width the
// recap uses. A resize replaces it with the terminal width minus the border.
type Options struct {
	Width  int
	Height int
	Color  bool
}

// Model is the bubbletea model for the week browser.
type Model struct {
	browser  report.Browser
	screen   screen
	cursor   [3]int
	offset   [3]int
	width    int
	height   int
	quitting bool
	style    report.Style
}

// NewModel returns a browser positioned on the first day.
func NewModel(b report.Browser, opt Options) Model {
	if b.Days == nil {
		b.Days = []report.BrowserDay{}
	}
	width := opt.Width
	if width <= 0 {
		width = report.DefaultRecapWidth
	}
	height := opt.Height
	if height <= 0 {
		height = 24
	}
	return Model{
		browser: b,
		width:   width,
		height:  height,
		style:   report.NewStyle(opt.Color),
	}.follow()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model. Keys move the cursor, drill from day to item
// to linker links, step back, and quit.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = frameWidth(msg.Width)
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		return m.follow(), nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "esc":
		if m.screen == screenDays {
			return m.quit()
		}
		return m.back(), nil
	case "backspace", "h", "left":
		return m.back(), nil
	case "j", "down":
		return m.move(1), nil
	case "k", "up":
		return m.move(-1), nil
	case "g", "home":
		return m.jump(0), nil
	case "G", "end":
		return m.jump(m.count() - 1), nil
	case "ctrl+d", "pgdown":
		return m.move(m.page()), nil
	case "ctrl+u", "pgup":
		return m.move(-m.page()), nil
	case "enter", "l", "right":
		return m.open(), nil
	default:
		return m, nil
	}
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

func (m Model) open() Model {
	switch m.screen {
	case screenDays:
		if m.count() == 0 {
			return m
		}
		m.screen = screenItems
		m.cursor[screenItems] = 0
		m.offset[screenItems] = 0
	case screenItems:
		if m.count() == 0 {
			return m
		}
		m.screen = screenLinks
		m.cursor[screenLinks] = 0
		m.offset[screenLinks] = 0
	default:
		return m
	}
	return m.follow()
}

func (m Model) back() Model {
	switch m.screen {
	case screenLinks:
		m.screen = screenItems
	case screenItems:
		m.screen = screenDays
	default:
		return m
	}
	return m.follow()
}

func (m Model) move(delta int) Model {
	if delta == 0 || m.count() == 0 {
		return m.follow()
	}
	m.cursor[m.screen] += delta
	return m.follow()
}

func (m Model) jump(index int) Model {
	m.cursor[m.screen] = index
	return m.follow()
}

func (m Model) follow() Model {
	n := m.count()
	if n == 0 {
		m.cursor[m.screen] = 0
		m.offset[m.screen] = 0
		return m
	}
	c := m.cursor[m.screen]
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	m.cursor[m.screen] = c
	off := m.offset[m.screen]
	if off < 0 {
		off = 0
	}
	page := m.page()
	if c < off {
		off = c
	}
	if c >= off+page {
		off = c - page + 1
	}
	maxOff := n - page
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	m.offset[m.screen] = off
	return m
}

func (m Model) count() int {
	switch m.screen {
	case screenDays:
		return len(m.browser.Days)
	case screenItems:
		day, ok := m.currentDay()
		if !ok {
			return 0
		}
		return len(day.Items)
	case screenLinks:
		item, ok := m.currentItem()
		if !ok {
			return 0
		}
		return len(item.Links)
	default:
		return 0
	}
}

func (m Model) page() int {
	h := m.height
	if h <= 0 {
		h = 24
	}
	n := h - viewHeader - viewFooter - viewBorder
	if n < 1 {
		n = 1
	}
	return n
}

func (m Model) currentDay() (report.BrowserDay, bool) {
	if len(m.browser.Days) == 0 {
		return report.BrowserDay{}, false
	}
	return m.browser.Days[m.dayIndex()], true
}

func (m Model) currentItem() (report.BrowserItem, bool) {
	day, ok := m.currentDay()
	if !ok || len(day.Items) == 0 {
		return report.BrowserItem{}, false
	}
	i := m.cursor[screenItems]
	if i < 0 {
		i = 0
	}
	if i >= len(day.Items) {
		i = len(day.Items) - 1
	}
	return day.Items[i], true
}

func (m Model) dayIndex() int {
	n := len(m.browser.Days)
	if n == 0 {
		return 0
	}
	i := m.cursor[screenDays]
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// frameWidth is the recap content width for a terminal that is term columns
// wide. The border takes one column on each side, and the width stays in
// the same 48–100 range as weeklog week.
func frameWidth(term int) int {
	inner := term - 2
	if inner < 48 {
		return 48
	}
	if inner > 100 {
		return 100
	}
	return inner
}
