package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

const (
	sourceGit    = "git"
	sourceClaude = "claude-code"
	sourceCodex  = "codex"
	sourceGitHub = "github"
	sourceLinear = "linear"
	sourceCursor = "cursor-cloud"
)

func runInitForm(cmd *cobra.Command, ctx context.Context, path string) error {
	choices, detected, err := config.Detect(ctx)
	if err != nil {
		return err
	}
	existed, err := config.ConfigExists(path)
	if err != nil {
		return err
	}
	answers, replace, err := askInitForm(ctx, cmd.InOrStdin(), cmd.ErrOrStderr(), choices, detected, existed, path)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return errInitCancelled
		}
		return err
	}
	result, err := config.WriteInitConfig(path, answers, replace)
	result.Sources = mergeInitStatuses(detected, answers)
	if textErr := printInitResult(cmd.OutOrStdout(), result); textErr != nil {
		return textErr
	}
	return err
}

func askInitForm(ctx context.Context, in io.Reader, out io.Writer, choices config.InitChoices, detected []config.SourceStatus, existed bool, path string) (config.InitChoices, bool, error) {
	selected := selectedSources(choices)
	var githubToken, linearToken, cursorToken string
	outputDir := choices.OutputDir
	outputMode := choices.OutputMode
	if outputMode == "" {
		outputMode = config.OutputModeBoth
	}
	var replace bool

	found := indexStatuses(detected)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Sources").
				Description("Space toggles a source. Enter continues.").
				Filterable(false).
				Options(
					sourceOption("Git", sourceGit, found[sourceGit], choices.Git),
					sourceOption("Claude Code", sourceClaude, found[sourceClaude], choices.Claude),
					sourceOption("Codex", sourceCodex, found[sourceCodex], choices.Codex),
					sourceOption("GitHub", sourceGitHub, found[sourceGitHub], choices.GitHubSource),
					sourceOption("Linear", sourceLinear, found[sourceLinear], false),
					sourceOption("Cursor Cloud", sourceCursor, found[sourceCursor], false),
				).
				Value(&selected),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("GitHub token").
				Description("Leave blank to use gh auth token. A pasted token is stored as a literal.").
				EchoMode(huh.EchoModePassword).
				Placeholder("gh auth token").
				Validate(func(s string) error {
					return config.ValidateLiteralToken(s, false)
				}).
				Value(&githubToken),
		).WithHideFunc(func() bool {
			return !containsSource(selected, sourceGitHub)
		}),
		huh.NewGroup(
			huh.NewInput().
				Title("Linear token").
				Description("Paste your Linear API key. It is stored in config.toml and not printed.").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					return config.ValidateLiteralToken(s, true)
				}).
				Value(&linearToken),
		).WithHideFunc(func() bool {
			return !containsSource(selected, sourceLinear)
		}),
		huh.NewGroup(
			huh.NewInput().
				Title("Cursor Cloud token").
				Description("Paste your Cursor API key. It is stored as a literal in config.toml.").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					return config.ValidateLiteralToken(s, true)
				}).
				Value(&cursorToken),
		).WithHideFunc(func() bool {
			return !containsSource(selected, sourceCursor)
		}),
		huh.NewGroup(
			huh.NewInput().
				Title("Output directory").
				Description("Weekly notes are written here. One config.toml is written, mode 0600.").
				Validate(config.ValidateOutputDir).
				Value(&outputDir),
			huh.NewSelect[string]().
				Title("Output mode").
				Options(
					huh.NewOption("both — write the file and print the recap", config.OutputModeBoth),
					huh.NewOption("file — write the file only", config.OutputModeFile),
					huh.NewOption("terminal — print only", config.OutputModeTerminal),
				).
				Value(&outputMode),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Replace the existing config?").
				Description(path).
				Affirmative("Replace").
				Negative("Keep").
				Value(&replace),
		).WithHideFunc(func() bool {
			return !existed
		}),
	).WithInput(in).WithOutput(out)

	if err := form.RunWithContext(ctx); err != nil {
		return config.InitChoices{}, false, err
	}

	next := choices
	next.Git = containsSource(selected, sourceGit)
	next.Claude = containsSource(selected, sourceClaude)
	next.Codex = containsSource(selected, sourceCodex)
	next.GitHubSource = containsSource(selected, sourceGitHub)
	next.GitHubToken = ""
	if next.GitHubSource {
		next.GitHubToken = githubToken
	}
	next.Linear = containsSource(selected, sourceLinear)
	next.LinearToken = ""
	if next.Linear {
		next.LinearToken = linearToken
	}
	next.CursorCloud = containsSource(selected, sourceCursor)
	next.CursorToken = ""
	if next.CursorCloud {
		next.CursorToken = cursorToken
	}
	next.OutputDir = outputDir
	next.OutputMode = outputMode
	if !existed {
		replace = false
	}
	return next, replace, nil
}

func selectedSources(c config.InitChoices) []string {
	var selected []string
	if c.Git {
		selected = append(selected, sourceGit)
	}
	if c.Claude {
		selected = append(selected, sourceClaude)
	}
	if c.Codex {
		selected = append(selected, sourceCodex)
	}
	if c.GitHubSource {
		selected = append(selected, sourceGitHub)
	}
	return selected
}

func sourceOption(label, value string, status config.SourceStatus, selected bool) huh.Option[string] {
	key := label
	if detail := strings.TrimSpace(status.Detail); detail != "" {
		key = fmt.Sprintf("%s (%s)", label, detail)
	}
	return huh.NewOption(key, value).Selected(selected)
}

func indexStatuses(sources []config.SourceStatus) map[string]config.SourceStatus {
	out := make(map[string]config.SourceStatus, len(sources))
	for _, src := range sources {
		out[src.Type] = src
	}
	return out
}

func containsSource(sources []string, want string) bool {
	for _, src := range sources {
		if src == want {
			return true
		}
	}
	return false
}

func mergeInitStatuses(detected []config.SourceStatus, choices config.InitChoices) []config.SourceStatus {
	enabled := map[string]bool{
		sourceGit:    choices.Git,
		sourceClaude: choices.Claude,
		sourceCodex:  choices.Codex,
		sourceGitHub: choices.GitHubSource,
		sourceLinear: choices.Linear,
		sourceCursor: choices.CursorCloud,
	}
	found := indexStatuses(detected)
	order := []string{sourceGit, sourceClaude, sourceCodex, sourceGitHub, sourceLinear, sourceCursor}
	out := make([]config.SourceStatus, 0, len(order))
	for _, typ := range order {
		on := enabled[typ]
		detail := "not selected"
		if on {
			detail = "selected"
			if prev, ok := found[typ]; ok && prev.Enabled && prev.Detail != "" {
				detail = prev.Detail
			}
		}
		out = append(out, config.SourceStatus{Type: typ, Enabled: on, Detail: detail})
	}
	return out
}
