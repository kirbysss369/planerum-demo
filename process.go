package hours

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var errProcessOutputLimit = errors.New("external process output limit exceeded")

// processOutput retains only the configured number of bytes and stops the
// process as soon as either output stream exceeds its limit.
type processOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (o *processOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	remaining := o.limit - o.data.Len()
	if remaining > 0 {
		o.data.Write(p[:min(remaining, len(p))])
	}
	if len(p) > remaining && !o.overflow {
		o.overflow = true
		o.cancel()
	}
	return len(p), nil
}

func (o *processOutput) result() ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return bytes.Clone(o.data.Bytes()), o.overflow
}

// runProcess runs one process with a deadline even when its caller has none.
// Errors deliberately omit command arguments, paths, and subprocess output:
// callers may show them in an HTTP response.
func runProcess(ctx context.Context, timeout time.Duration, stdoutLimit, stderrLimit int, name string, args ...string) ([]byte, error) {
	if timeout <= 0 || stdoutLimit <= 0 || stderrLimit <= 0 {
		return nil, errors.New("external process has invalid limits")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &processOutput{limit: stdoutLimit, cancel: cancel}
	stderr := &processOutput{limit: stderrLimit, cancel: cancel}
	command := exec.CommandContext(runCtx, name, args...)
	command.WaitDelay = time.Second
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	output, stdoutOverflow := stdout.result()
	_, stderrOverflow := stderr.result()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("external process: %w", ctx.Err())
	}
	if stdoutOverflow || stderrOverflow {
		return nil, errProcessOutputLimit
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("external process: %w", context.DeadlineExceeded)
	}
	if err != nil {
		return nil, errors.New("external process failed")
	}
	return output, nil
}
