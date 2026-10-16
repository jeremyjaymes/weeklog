package github

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

func (s *Source) resolveToken(ctx context.Context, config map[string]any) (string, error) {
	token, ok, err := configuredToken(config)
	if err != nil {
		return "", err
	}
	if !ok {
		token, err = s.tokenFromGH(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", configErr("token is unset and gh auth token is unavailable")
		}
	}
	token = cleanToken(token)
	if token == "" {
		return "", configErr("token is unset and gh auth token is unavailable")
	}
	if strings.IndexFunc(token, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	}) >= 0 {
		return "", configErr("token contains whitespace or control characters")
	}
	return token, nil
}

func configuredToken(config map[string]any) (string, bool, error) {
	if config == nil {
		return "", false, nil
	}
	value, ok := config["token"]
	if !ok || value == nil {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", false, configErr("token must be a string")
	}
	text = cleanToken(text)
	if text == "" {
		return "", false, nil
	}
	return text, true, nil
}

func (s *Source) tokenFromGH(ctx context.Context) (string, error) {
	if s != nil && s.ghToken != nil {
		return s.ghToken(ctx)
	}
	return ghAuthToken(ctx)
}

// ghAuthToken runs `gh auth token` and returns the trimmed stdout.
// Stderr is discarded. The token is never placed in an error string.
func ghAuthToken(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "auth", "token")
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// nil would inherit the parent stderr. gh writes the token to stdout;
	// anything it prints on stderr must not reach the terminal or a log.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", err
	}
	token := cleanToken(stdout.String())
	if token == "" {
		return "", errors.New("empty token")
	}
	return token, nil
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
