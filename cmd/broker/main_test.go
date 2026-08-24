package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestMainInvokesRuntime(t *testing.T) {
	original := run
	defer func() { run = original }()

	called := false
	run = func(ctx context.Context, logger *slog.Logger) error {
		called = ctx != nil && logger != nil
		return nil
	}
	main()
	if !called {
		t.Fatal("main() did not invoke runtime with context and logger")
	}
}

func TestMainLogsRuntimeFailure(t *testing.T) {
	original := run
	defer func() { run = original }()

	run = func(context.Context, *slog.Logger) error {
		return errors.New("expected")
	}
	main()
}

func TestMainPrintsVersion(t *testing.T) {
	originalRun := run
	originalArgs := commandArgs
	defer func() { run = originalRun; commandArgs = originalArgs }()

	called := false
	run = func(ctx context.Context, logger *slog.Logger) error {
		called = true
		return nil
	}
	commandArgs = []string{"broker", "--version"}

	main()

	if called {
		t.Fatal("main() invoked the runtime for --version")
	}
}
