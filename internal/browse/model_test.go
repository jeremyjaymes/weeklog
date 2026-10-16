package browse

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/report"
)

func TestModelUpdateMovesAndClamps(t *testing.T) {
	m := NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	if m.screen != screenDays || m.cursor[screenDays] != 0 || m.offset[screenDays] != 0 {
		t.Fatalf("start = screen %d cursor %d offset %d", m.screen, m.cursor[screenDays], m.offset[screenDays])
	}
	if cmd := command(t, &m, key("j")); cmd != nil {
		t.Fatal("move returned a command")
	}
	if m.cursor[screenDays] != 1 {
		t.Fatalf("cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, keyType(tea.KeyDown))
	if m.cursor[screenDays] != 2 {
		t.Fatalf("cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, key("k"))
	command(t, &m, keyType(tea.KeyUp))
	if m.cursor[screenDays] != 0 {
		t.Fatalf("cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, key("k"))
	if m.cursor[screenDays] != 0 {
		t.Fatalf("cursor moved above the list: %d", m.cursor[screenDays])
	}
	command(t, &m, key("G"))
	if m.cursor[screenDays] != 2 {
		t.Fatalf("end cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, key("j"))
	if m.cursor[screenDays] != 2 {
		t.Fatalf("cursor moved past the list: %d", m.cursor[screenDays])
	}
	command(t, &m, key("g"))
	command(t, &m, keyType(tea.KeyEnd))
	if m.cursor[screenDays] != 2 {
		t.Fatalf("end key cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, keyType(tea.KeyHome))
	if m.cursor[screenDays] != 0 {
		t.Fatalf("home cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, key("x"))
	if m.cursor[screenDays] != 0 || m.screen != screenDays {
		t.Fatalf("unknown key changed state: screen %d cursor %d", m.screen, m.cursor[screenDays])
	}
}

func TestModelUpdateDrillAndBack(t *testing.T) {
	m := NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	command(t, &m, key("j"))
	command(t, &m, keyType(tea.KeyEnter))
	if m.screen != screenItems || m.cursor[screenItems] != 0 || m.cursor[screenDays] != 1 {
		t.Fatalf("after day: screen %d item %d day %d", m.screen, m.cursor[screenItems], m.cursor[screenDays])
	}
	command(t, &m, key("l"))
	if m.screen != screenLinks || m.cursor[screenLinks] != 0 || m.cursor[screenItems] != 0 {
		t.Fatalf("after item: screen %d link %d item %d", m.screen, m.cursor[screenLinks], m.cursor[screenItems])
	}
	command(t, &m, key("j"))
	command(t, &m, keyType(tea.KeyEnter))
	if m.screen != screenLinks || m.cursor[screenLinks] != 1 {
		t.Fatalf("enter on a link drilled further: screen %d cursor %d", m.screen, m.cursor[screenLinks])
	}
	command(t, &m, keyType(tea.KeyEsc))
	if m.screen != screenItems || m.cursor[screenItems] != 0 {
		t.Fatalf("back from links: screen %d cursor %d", m.screen, m.cursor[screenItems])
	}
	command(t, &m, key("j"))
	if m.cursor[screenItems] != 1 {
		t.Fatalf("item cursor = %d", m.cursor[screenItems])
	}
	command(t, &m, key("h"))
	if m.screen != screenDays || m.cursor[screenDays] != 1 {
		t.Fatalf("back from items: screen %d cursor %d", m.screen, m.cursor[screenDays])
	}
	command(t, &m, key("left"))
	if m.screen != screenDays || m.quitting {
		t.Fatalf("left at the day list changed state: screen %d quit %v", m.screen, m.quitting)
	}

	command(t, &m, key("g"))
	command(t, &m, key("enter"))
	if m.screen != screenItems || m.count() != 0 {
		t.Fatalf("empty day: screen %d count %d", m.screen, m.count())
	}
	command(t, &m, key("enter"))
	if m.screen != screenItems {
		t.Fatalf("enter on an empty day drilled further: screen %d", m.screen)
	}
	command(t, &m, key("backspace"))
	if m.screen != screenDays {
		t.Fatalf("backspace screen = %d", m.screen)
	}

	command(t, &m, key("j"))
	command(t, &m, key("right"))
	command(t, &m, key("j"))
	command(t, &m, key("enter"))
	if m.screen != screenLinks || m.count() != 0 {
		t.Fatalf("unlinked item: screen %d count %d", m.screen, m.count())
	}
}

func TestModelUpdateQuit(t *testing.T) {
	m := NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	if m.Init() != nil {
		t.Fatal("Init returned a command")
	}
	cmd := command(t, &m, key("q"))
	if !m.quitting || !isQuit(cmd) || m.View() != "" {
		t.Fatalf("q quit = %v cmd %v view %q", m.quitting, isQuit(cmd), m.View())
	}

	m = NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	command(t, &m, key("j"))
	command(t, &m, key("enter"))
	cmd = command(t, &m, keyType(tea.KeyCtrlC))
	if !m.quitting || !isQuit(cmd) {
		t.Fatal("ctrl+c did not quit from the item list")
	}

	m = NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	cmd = command(t, &m, keyType(tea.KeyEsc))
	if !m.quitting || !isQuit(cmd) {
		t.Fatal("esc did not quit from the day list")
	}

	m = NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	command(t, &m, key("enter"))
	cmd = command(t, &m, keyType(tea.KeyEsc))
	if m.quitting || isQuit(cmd) || m.screen != screenDays {
		t.Fatalf("esc from an empty day quit: quit %v screen %d", m.quitting, m.screen)
	}
}

func TestModelUpdateScroll(t *testing.T) {
	days := make([]report.BrowserDay, 8)
	for i := range days {
		days[i] = report.BrowserDay{Label: "Day", When: "Oct"}
	}
	m := NewModel(report.Browser{Heading: "Week", Days: days}, Options{Width: 80, Height: 10, Color: false})
	if m.page() != 3 {
		t.Fatalf("page = %d", m.page())
	}
	for i := 0; i < 5; i++ {
		command(t, &m, key("j"))
	}
	if m.cursor[screenDays] != 5 || m.offset[screenDays] != 3 {
		t.Fatalf("cursor %d offset %d", m.cursor[screenDays], m.offset[screenDays])
	}
	command(t, &m, key("k"))
	if m.cursor[screenDays] != 4 || m.offset[screenDays] != 3 {
		t.Fatalf("after up cursor %d offset %d", m.cursor[screenDays], m.offset[screenDays])
	}
	command(t, &m, key("g"))
	command(t, &m, keyType(tea.KeyPgDown))
	if m.cursor[screenDays] != 3 || m.offset[screenDays] != 1 {
		t.Fatalf("pgdown cursor %d offset %d", m.cursor[screenDays], m.offset[screenDays])
	}
	command(t, &m, key("ctrl+d"))
	command(t, &m, key("ctrl+d"))
	if m.cursor[screenDays] != 7 {
		t.Fatalf("ctrl+d cursor = %d", m.cursor[screenDays])
	}
	command(t, &m, key("ctrl+u"))
	if m.cursor[screenDays] != 4 {
		t.Fatalf("ctrl+u cursor = %d", m.cursor[screenDays])
	}

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = modelOf(t, next)
	if cmd != nil {
		t.Fatal("resize returned a command")
	}
	if m.width != 98 || m.height != 24 || m.offset[screenDays] != 0 || m.cursor[screenDays] != 4 {
		t.Fatalf("resize width %d height %d cursor %d offset %d", m.width, m.height, m.cursor[screenDays], m.offset[screenDays])
	}
	command(t, &m, key("G"))
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = modelOf(t, next)
	if m.cursor[screenDays] != 7 || m.offset[screenDays] != 5 {
		t.Fatalf("shrunk cursor %d offset %d", m.cursor[screenDays], m.offset[screenDays])
	}
}

func TestModelViewDrilldown(t *testing.T) {
	m := NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	view := m.View()
	if strings.Contains(view, "\x1b") {
		t.Fatal("plain view contains ANSI")
	}
	for _, want := range []string{"Week 2026-W41", "Days", "Mon  Oct 5", "—", "Thu  Oct 8", "2 items", "╭", "1/3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("day view missing %q\n%s", want, view)
		}
	}
	if !strings.HasPrefix(stripBorder(view), "> Mon") && !strings.Contains(view, "> Mon  Oct 5") {
		t.Fatalf("first day is not selected\n%s", view)
	}

	command(t, &m, key("j"))
	command(t, &m, key("enter"))
	view = m.View()
	for _, want := range []string{"Thu  Oct 8", "Items", "Add the gate", "Shipped", "Claude", "3 links", "Loose notes", "not linked", "1/2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("item view missing %q\n%s", want, view)
		}
	}

	command(t, &m, key("enter"))
	view = m.View()
	for _, want := range []string{"Add the gate", "Linked", "* Claude", "Admin gate", "ref:GEN-1 in prompt", "aaaaaaa", "#8", "1/3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("link view missing %q\n%s", want, view)
		}
	}

	command(t, &m, key("esc"))
	command(t, &m, key("j"))
	command(t, &m, key("enter"))
	view = m.View()
	if !strings.Contains(view, "No linker-linked commits, pull requests, or sessions") {
		t.Fatalf("unlinked view = %s", view)
	}

	m = NewModel(sampleBrowser(), Options{Width: 80, Height: 24, Color: false})
	command(t, &m, key("G"))
	command(t, &m, key("enter"))
	command(t, &m, key("enter"))
	view = m.View()
	for _, want := range []string{"o Codex", "files overlap: 1", "bbbbbbb"} {
		if !strings.Contains(view, want) {
			t.Fatalf("soft link view missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "* Codex") {
		t.Fatalf("soft link used a hard mark\n%s", view)
	}
}

func TestModelUpdateEmptyWeek(t *testing.T) {
	m := NewModel(report.Browser{Heading: "Week"}, Options{Width: 72, Height: 24, Color: false})
	command(t, &m, key("j"))
	command(t, &m, key("enter"))
	if m.screen != screenDays || m.cursor[screenDays] != 0 {
		t.Fatalf("empty week screen %d cursor %d", m.screen, m.cursor[screenDays])
	}
	if !strings.Contains(m.View(), "No days in this window") {
		t.Fatalf("view = %s", m.View())
	}
	cmd := command(t, &m, key("q"))
	if !isQuit(cmd) {
		t.Fatal("q did not quit an empty week")
	}
}

func sampleBrowser() report.Browser {
	return report.Browser{
		Heading: "Week 2026-W41",
		Days: []report.BrowserDay{
			{Date: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), Label: "Mon", When: "Oct 5"},
			{
				Date: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), Label: "Thu", When: "Oct 8",
				Items: []report.BrowserItem{
					{
						Title: "Add the gate", Status: "Shipped", Actor: "Claude", Repo: "app",
						Links: []report.BrowserLink{
							{Kind: event.KindAgentSession, Label: "Claude", Detail: "Admin gate", Reason: "ref:GEN-1 in prompt"},
							{Kind: event.KindCommit, Label: "aaaaaaa", Detail: "Add the gate", Reason: "ref:GEN-1 in commit message"},
							{Kind: event.KindPR, Label: "#8", Detail: "Add the gate", Reason: "ref:GEN-1 in title"},
						},
					},
					{Title: "Loose notes", Status: "Explored", Actor: "Human", Repo: "app"},
				},
			},
			{
				Date: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Label: "Fri", When: "Oct 9",
				Items: []report.BrowserItem{{
					Title: "Spike", Status: "Explored", Actor: "Codex", Repo: "otto",
					Links: []report.BrowserLink{
						{Kind: event.KindAgentSession, Label: "Codex", Detail: "Spike", Reason: "files overlap: 1", Soft: true},
						{Kind: event.KindCommit, Label: "bbbbbbb", Detail: "Try the spike", Reason: "files overlap: 1", Soft: true},
					},
				}},
			},
		},
	}
}

func command(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	next, cmd := m.Update(msg)
	*m = modelOf(t, next)
	return cmd
}

func modelOf(t *testing.T, next tea.Model) Model {
	t.Helper()
	m, ok := next.(Model)
	if !ok {
		t.Fatalf("model type %T", next)
	}
	return m
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(kind tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: kind}
}

func stripBorder(view string) string {
	view = strings.TrimSpace(view)
	if i := strings.IndexByte(view, '\n'); i >= 0 {
		view = view[i+1:]
	}
	return strings.TrimSpace(view)
}
