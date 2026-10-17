package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cuentica-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testBinary = filepath.Join(dir, "cuentica")
	if runtime.GOOS == "windows" {
		testBinary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	build := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.Version=1.2.3 -X main.Commit=abc123 -X main.BuildDate=2026-10-17T00:00:00Z", "-o", testBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI: %v\n%s", err, output)
		cancel()
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	// Warm up the executable so platform launch delays cannot race signal tests.
	initialize := exec.CommandContext(ctx, testBinary, "--version")
	initialize.Env = cleanEnvironment()
	if output, err := initialize.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "initialize CLI: %v\n%s", err, output)
		cancel()
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	cancel()
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCLICommands(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		code       int
		stdoutWant []string
		stderrWant []string
	}{
		{name: "summary", stdoutWant: []string{"cuentica", "--help"}},
		{name: "help", args: []string{"--help"}, stdoutWant: []string{"cuentica", "auth"}},
		{name: "version", args: []string{"--version"}, stdoutWant: []string{"cuentica 1.2.3 (commit abc123, built 2026-10-17T00:00:00Z)"}},
		{name: "unknown command", args: []string{"nonsense", "--output", "text"}, code: 2, stderrWant: []string{"CLI-001", "Unknown command", "--help"}},
		{name: "unknown flag", args: []string{"--nonexistent-flag"}, code: 2, stderrWant: []string{"CLI-001", "unknown flag"}},
		{name: "invalid format", args: []string{"--output", "xml"}, code: 2, stderrWant: []string{"CLI-001", "--output"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, testBinary, tt.args...)
			command.Env = cleanEnvironment()
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if got := exitCode(err); got != tt.code {
				t.Fatalf("exit = %d, want %d; stderr: %s", got, tt.code, stderr.String())
			}
			for _, want := range tt.stdoutWant {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q: %q", want, stdout.String())
				}
			}
			for _, want := range tt.stderrWant {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q: %q", want, stderr.String())
				}
			}
			if len(tt.stderrWant) == 0 && stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %q", stderr.String())
			}
			if len(tt.stdoutWant) == 0 && stdout.Len() != 0 {
				t.Errorf("unexpected stdout: %q", stdout.String())
			}
			if strings.Contains(stderr.String(), "goroutine ") || strings.Contains(stderr.String(), "panic:") {
				t.Errorf("stack trace on stderr: %q", stderr.String())
			}
		})
	}
}

func TestCLISignalsExitWhileInputIsBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signal exit codes are unavailable on Windows")
	}
	for _, tt := range []struct {
		name   string
		signal os.Signal
		code   int
	}{
		{"interrupt", os.Interrupt, 130},
		{"terminate", syscall.SIGTERM, 143},
		{"broken pipe", syscall.SIGPIPE, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.signal == syscall.SIGPIPE && (runtime.GOOS == "darwin" || runtime.GOOS == "ios") {
				t.Skip("Go ignores externally delivered SIGPIPE on Darwin; closed-stdout test covers real broken pipes")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, testBinary, "auth", "login", "--key-stdin")
			command.Env = cleanEnvironment()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			command.Stdin = reader
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			// Let signal handlers install while stdin remains blocked.
			select {
			case err := <-done:
				t.Fatalf("CLI exited before signal: %v; stderr: %s", err, stderr.String())
			case <-time.After(200 * time.Millisecond):
			}
			if err := command.Process.Signal(tt.signal); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if got := exitCode(err); got != tt.code {
					t.Fatalf("exit = %d, want %d; stderr: %s", got, tt.code, stderr.String())
				}
			case <-ctx.Done():
				t.Fatal("CLI did not exit after signal while input was blocked")
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("signal emitted output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCLIClosedStdoutExitsSilently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix broken-pipe handling is unavailable on Windows")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, testBinary, "--help")
	command.Env = cleanEnvironment()
	command.Stdout = writer
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); exitCode(err) != 0 {
		t.Fatalf("closed stdout: %v; stderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("broken pipe emitted stderr: %q", stderr.String())
	}
}

func cleanEnvironment() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CUENTICA_API_KEY=") {
			environment = append(environment, entry)
		}
	}
	return environment
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var failure *exec.ExitError
	if errors.As(err, &failure) {
		return failure.ExitCode()
	}
	return -1
}
