package hours

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const processHelperFlag = "-test.run=^TestProcessHelper$"

func TestProcessHelper(t *testing.T) {
	if len(os.Args) < 4 || os.Args[1] != processHelperFlag || os.Args[2] != "--" {
		return
	}
	switch os.Args[3] {
	case "block":
		if len(os.Args) > 4 {
			if err := os.WriteFile(os.Args[4], []byte("started"), 0o600); err != nil {
				os.Exit(2)
			}
		}
		time.Sleep(time.Hour)
	case "output":
		fmt.Fprint(os.Stdout, strings.Repeat("o", 4096))
	case "error-output":
		fmt.Fprint(os.Stderr, strings.Repeat("e", 4096))
	case "descendant":
		child := exec.Command(os.Args[0], processHelperFlag, "--", "short-lived-child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
	case "short-lived-child":
		time.Sleep(3 * time.Second)
	case "ok":
		fmt.Fprint(os.Stdout, "valid output")
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func helperArgs(mode string, extra ...string) []string {
	return append([]string{processHelperFlag, "--", mode}, extra...)
}

func TestRunProcessDeadlineAndCancellation(t *testing.T) {
	start := time.Now()
	_, err := runProcess(context.Background(), 50*time.Millisecond, 1024, 1024, os.Args[0], helperArgs("block")...)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("deadline error = %v, elapsed = %s", err, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := filepath.Join(t.TempDir(), "started")
	done := make(chan error, 1)
	go func() {
		_, err := runProcess(ctx, time.Minute, 1024, 1024, os.Args[0], helperArgs("block", started)...)
		done <- err
	}()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("helper exited before cancellation: %v", err)
		case <-deadline:
			t.Fatal("helper did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled process did not return")
	}
}

func TestRunProcessOutputLimits(t *testing.T) {
	for _, mode := range []string{"output", "error-output"} {
		t.Run(mode, func(t *testing.T) {
			_, err := runProcess(context.Background(), 3*time.Second, 32, 32, os.Args[0], helperArgs(mode)...)
			if !errors.Is(err, errProcessOutputLimit) {
				t.Fatalf("output limit error = %v", err)
			}
			if strings.Contains(err.Error(), strings.Repeat("o", 32)) || strings.Contains(err.Error(), strings.Repeat("e", 32)) {
				t.Fatalf("process output leaked: %v", err)
			}
		})
	}
	output, err := runProcess(context.Background(), 3*time.Second, 32, 32, os.Args[0], helperArgs("ok")...)
	if err != nil || string(output) != "valid output" {
		t.Fatalf("short output = %q, %v", output, err)
	}
}

func TestRunProcessWaitDelayClosesDescendantPipes(t *testing.T) {
	start := time.Now()
	_, err := runProcess(context.Background(), 5*time.Second, 1024, 1024, os.Args[0], helperArgs("descendant")...)
	if err == nil || time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("descendant pipe wait = %s, error = %v", time.Since(start), err)
	}
}

const fakeOfficeFlag = "-test.run=^TestFakeOfficeProcess$"

func TestFakeOfficeProcess(t *testing.T) {
	if len(os.Args) != 4 || os.Args[1] != fakeOfficeFlag {
		return
	}
	dir := os.Args[3]
	if err := os.WriteFile(filepath.Join(dir, "program.docx"), []byte("document"), 0o600); err != nil {
		os.Exit(2)
	}
	if os.Getenv("FAKE_OFFICE_MODE") == "fail" {
		fmt.Fprintln(os.Stderr, "secret-process-stderr", os.Args[2])
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(dir, "lectures.pptx"), []byte("slides"), 0o600); err != nil {
		os.Exit(2)
	}
}

func TestFailedExportCleansStagingAndExplicitRetrySucceeds(t *testing.T) {
	_, p := readyProgram(t)
	root := t.TempDir()
	exporter := Exporter{Root: root, Python: os.Args[0], Script: fakeOfficeFlag}
	t.Setenv("FAKE_OFFICE_MODE", "fail")
	_, err := exporter.Export(context.Background(), p)
	if err == nil || strings.Contains(err.Error(), "secret-process-stderr") || strings.Contains(err.Error(), root) {
		t.Fatalf("failed export error exposed process detail: %v", err)
	}
	parent := filepath.Join(root, p.ID)
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed export left partial bundle: %v, %v", entries, err)
	}
	t.Setenv("FAKE_OFFICE_MODE", "success")
	files, err := exporter.Export(context.Background(), p)
	if err != nil || len(files) != len(artifactNames) {
		t.Fatalf("explicit retry = %v, %v", files, err)
	}
	final := filepath.Join(parent, fmt.Sprintf("revision-%d", p.Revision))
	if !completeExport(final) {
		t.Fatal("retry did not publish a complete bundle")
	}
	entries, err = os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(final) {
		t.Fatalf("unexpected published files: %v, %v", entries, err)
	}
}
