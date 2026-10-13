package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Process implements Source by running a weeklog-source-<name> executable.
// Secrets travel only in the collect request on stdin. Argv is the executable
// path plus "describe" or "collect".
type Process struct {
	Name    string
	Path    string
	Timeout time.Duration
}

// Describe runs "<path> describe" and decodes one JSON document from stdout.
func (p *Process) Describe(ctx context.Context) (Manifest, error) {
	stdout, stderr, code, err := p.exec(ctx, "describe", nil, nil)
	if err != nil {
		return Manifest{}, err
	}
	if err := p.exitError("describe", code, stderr); err != nil {
		return Manifest{}, err
	}
	manifest, err := DecodeManifest(stdout)
	if err != nil {
		return Manifest{}, p.annotate("describe", err)
	}
	if manifest.Name != p.Name {
		return Manifest{}, &Failure{
			Name:   p.Name,
			Op:     "describe",
			Reason: "manifest name " + oneLine(manifest.Name),
			Err:    ErrProtocol,
		}
	}
	return manifest, nil
}

// Collect runs "<path> collect". Events are delivered only when the process
// exits 0. Invalid lines are counted on Result.Dropped and skipped.
func (p *Process) Collect(ctx context.Context, req CollectRequest, emit func(event.Event) error) (Result, error) {
	payload, err := EncodeRequest(req)
	if err != nil {
		return Result{}, p.annotate("collect", err)
	}
	var (
		result  Result
		pending []event.Event
	)
	_, stderr, code, err := p.exec(ctx, "collect", payload, func(r io.Reader) error {
		dropped, scanErr := parseCollect(p.Name, r, &result, &pending)
		result.Dropped += dropped
		return scanErr
	})
	if err != nil {
		return Result{}, err
	}
	if err := p.exitError("collect", code, stderr); err != nil {
		return Result{Dropped: result.Dropped}, err
	}
	if emit != nil {
		for _, ev := range pending {
			if err := emit(ev); err != nil {
				return result, err
			}
			result.Events++
		}
	} else {
		result.Events = len(pending)
	}
	return result, nil
}

func (p *Process) exitError(op string, code int, stderr string) error {
	switch code {
	case ExitOK:
		return nil
	case ExitConfig:
		return &Failure{Name: p.Name, Op: op, Code: code, Stderr: stderr, Reason: "plugin config error", Err: ErrConfig}
	default:
		return &Failure{Name: p.Name, Op: op, Code: code, Stderr: stderr, Reason: "plugin failed", Err: ErrFailed}
	}
}

// exec runs the plugin. onStdout consumes stdout when non-nil; otherwise
// stdout is returned. The stderr drain runs concurrently so a full stderr
// pipe cannot block the process.
func (p *Process) exec(ctx context.Context, op string, stdin []byte, onStdout func(io.Reader) error) ([]byte, string, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", 0, err
	}
	if p.Path == "" {
		return nil, "", 0, &Failure{Name: p.Name, Op: op, Reason: "plugin path is empty", Err: ErrFailed}
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, p.Path, op)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.WaitDelay = time.Second

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", 0, &Failure{Name: p.Name, Op: op, Reason: "cannot run plugin", Err: ErrFailed}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", 0, &Failure{Name: p.Name, Op: op, Reason: "cannot run plugin", Err: ErrFailed}
	}
	stderrBuf := &capBuffer{max: maxStderrBytes}
	errCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(stderrBuf, stderrPipe)
		close(errCh)
	}()

	if err := cmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		<-errCh
		return nil, "", 0, &Failure{Name: p.Name, Op: op, Reason: "cannot run plugin", Err: ErrFailed}
	}

	var stdout []byte
	var readErr error
	if onStdout != nil {
		readErr = onStdout(stdoutPipe)
	} else {
		stdout, readErr = readLimited(stdoutPipe, maxMessageBytes)
	}
	waitErr := cmd.Wait()
	<-errCh
	stderrText := stderrBuf.String()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, stderrText, 0, &Failure{
			Name:   p.Name,
			Op:     op,
			Stderr: stderrText,
			Reason: "timed out after " + p.timeout().String(),
			Err:    ErrTimeout,
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, stderrText, 0, ctx.Err()
	}
	if readErr != nil {
		return nil, stderrText, 0, p.annotate(op, readErr)
	}
	if waitErr == nil {
		return stdout, stderrText, ExitOK, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return stdout, stderrText, exitErr.ExitCode(), nil
	}
	return nil, stderrText, 0, &Failure{Name: p.Name, Op: op, Reason: "cannot run plugin", Err: ErrFailed}
}

func (p *Process) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return DefaultTimeout
}

func (p *Process) annotate(op string, err error) error {
	var failure *Failure
	if errors.As(err, &failure) {
		cp := *failure
		if cp.Name == "" {
			cp.Name = p.Name
		}
		if cp.Op == "" {
			cp.Op = op
		}
		return &cp
	}
	sentinel := ErrProtocol
	switch {
	case errors.Is(err, ErrConfig):
		sentinel = ErrConfig
	case errors.Is(err, ErrTimeout):
		sentinel = ErrTimeout
	case errors.Is(err, ErrFailed):
		sentinel = ErrFailed
	}
	return &Failure{Name: p.Name, Op: op, Reason: oneLine(err.Error()), Err: sentinel}
}

func readLimited(r io.Reader, max int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		_, _ = io.Copy(io.Discard, r)
		return nil, fmt.Errorf("%w: response exceeds %d bytes", ErrProtocol, max)
	}
	return data, nil
}

// capBuffer keeps at most max bytes. Extra input is discarded.
type capBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if c.max > 0 && c.buf.Len() < c.max {
		remain := c.max - c.buf.Len()
		if len(p) > remain {
			_, _ = c.buf.Write(p[:remain])
		} else {
			_, _ = c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *capBuffer) String() string {
	return c.buf.String()
}
