package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const (
	defaultCommandTimeout = 30 * time.Second
	defaultMaxSecretBytes = 1 << 20
)

// commandTimeout limits each cmd: secret resolution.
var commandTimeout = defaultCommandTimeout

// maxSecretBytes limits file: and cmd: secret payloads.
var maxSecretBytes = defaultMaxSecretBytes

// secretValue holds a resolved secret. Formatting it never reveals the value.
type secretValue struct {
	s string
}

func (v secretValue) Value() string { return v.s }

func (v secretValue) String() string { return "[redacted]" }

func (v secretValue) GoString() string { return "secretValue{[redacted]}" }

func (v secretValue) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "[redacted]")
}

type refKind int

const (
	refLiteral refKind = iota
	refEnv
	refCmd
	refFile
)

// secretRef is a parsed secret reference. Display is safe to print.
type secretRef struct {
	Kind    refKind
	Display string
	Env     string
	Command string
	Path    string
}

func parseSecretRef(raw string) (secretRef, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "env:"):
		name := strings.TrimSpace(strings.TrimPrefix(raw, "env:"))
		if !validEnvName(name) {
			return secretRef{Kind: refEnv, Display: "env:"}, fmt.Errorf("invalid environment variable name %s", quoteUser(name))
		}
		return secretRef{Kind: refEnv, Display: "env:" + name, Env: name}, nil
	case strings.HasPrefix(raw, "cmd:"):
		command := strings.TrimSpace(strings.TrimPrefix(raw, "cmd:"))
		if command == "" {
			return secretRef{Kind: refCmd, Display: "cmd:"}, errors.New("command is empty")
		}
		return secretRef{Kind: refCmd, Display: "cmd:" + oneLine(command), Command: command}, nil
	case strings.HasPrefix(raw, "file:"):
		path := strings.TrimSpace(strings.TrimPrefix(raw, "file:"))
		if path == "" {
			return secretRef{Kind: refFile, Display: "file:"}, errors.New("file path is empty")
		}
		return secretRef{Kind: refFile, Display: "file:" + oneLine(path), Path: path}, nil
	default:
		return secretRef{Kind: refLiteral, Display: "literal"}, nil
	}
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func resolveSecretRef(ctx context.Context, ref secretRef, configDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.New("canceled")
	}
	switch ref.Kind {
	case refEnv:
		return resolveEnv(ref.Env)
	case refCmd:
		return runCommand(ctx, ref.Command)
	case refFile:
		path, err := resolveSecretFile(configDir, ref.Path)
		if err != nil {
			return "", err
		}
		return readSecretFile(path)
	default:
		return "", errors.New("literal secrets are not resolved")
	}
}

func resolveEnv(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s is unset", name)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is empty", name)
	}
	return value, nil
}

func resolveSecretFile(configDir, raw string) (string, error) {
	expanded, err := expandUser(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", errors.New("file path is empty")
	}
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}
	if configDir == "" {
		configDir = "."
	}
	return filepath.Clean(filepath.Join(configDir, expanded)), nil
}

func readSecretFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("file does not exist")
		}
		return "", errors.New("cannot read file")
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return "", errors.New("cannot read file")
	}
	if info.IsDir() {
		_ = f.Close()
		return "", errors.New("file path is a directory")
	}
	if info.Size() > int64(maxSecretBytes) {
		_ = f.Close()
		return "", fmt.Errorf("file exceeds %d bytes", maxSecretBytes)
	}
	buf, err := io.ReadAll(io.LimitReader(f, int64(maxSecretBytes)+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", errors.New("cannot read file")
	}
	if len(buf) > maxSecretBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxSecretBytes)
	}
	value := strings.TrimSpace(string(buf))
	if value == "" {
		return "", errors.New("file is empty")
	}
	return value, nil
}

func runCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}
	cmd := exec.CommandContext(ctx, shell, flag, command)
	stdout := &capBuffer{max: maxSecretBytes}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", errors.New("command timed out")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "", errors.New("command canceled")
	}
	if stdout.overflow {
		return "", fmt.Errorf("command output exceeds %d bytes", maxSecretBytes)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("command exited %d", exitErr.ExitCode())
		}
		return "", errors.New("command failed")
	}
	value := strings.TrimSpace(stdout.buf.String())
	if value == "" {
		return "", errors.New("command produced empty output")
	}
	return value, nil
}

// capBuffer keeps at most max bytes. Extra input is discarded so a command
// cannot fill memory, and discarded bytes are never returned.
type capBuffer struct {
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if !c.overflow && c.buf.Len() < c.max {
		remain := c.max - c.buf.Len()
		if len(p) > remain {
			_, _ = c.buf.Write(p[:remain])
			c.overflow = true
		} else {
			_, _ = c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.overflow = true
	}
	return len(p), nil
}

// oneLine replaces control characters so config values cannot break the report.
func oneLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func quoteUser(s string) string {
	s = oneLine(s)
	const max = 200
	if len(s) > max {
		s = s[:max] + "..."
	}
	return fmt.Sprintf("%q", s)
}
