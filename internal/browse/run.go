package browse

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeremyjaymes/weeklog/internal/report"
)

// Run opens the week browser on out and blocks until the user quits.
// The caller checks that in and out are a terminal. The alt screen is
// restored on the way out.
func Run(ctx context.Context, out io.Writer, b report.Browser, opt Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	prog := tea.NewProgram(NewModel(b, opt), tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(out))
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("interactive week: %w", err)
	}
	return nil
}
