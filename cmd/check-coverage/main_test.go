package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		arguments  []string
		output     string
		err        error
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "arguments rejected",
			arguments:  []string{"unexpected"},
			wantCode:   2,
			wantStderr: "usage: check-coverage\n",
		},
		{
			name:       "command failure",
			output:     "partial output",
			err:        errors.New("failed"),
			wantCode:   1,
			wantStdout: "partial output",
			wantStderr: "run Go coverage: failed\n",
		},
		{
			name:       "missing tests and coverage",
			output:     "package/a\t[no test files]\npackage/b\tcoverage: 50.0% of statements\n",
			wantCode:   1,
			wantStdout: "package/a\t[no test files]\npackage/b\tcoverage: 50.0% of statements\n",
			wantStderr: "every Go package must contain at least one _test.go file:\n" +
				"package/a\t[no test files]\n" +
				"every executable Go package must reach 100.0% statement coverage:\n" +
				"package/b\tcoverage: 50.0% of statements\n",
		},
		{
			name:       "complete coverage",
			output:     "package/a\tcoverage: 100.0% of statements\n",
			wantCode:   0,
			wantStdout: "package/a\tcoverage: 100.0% of statements\nAll Go packages contain test files and all executable Go packages reached 100.0% statement coverage.\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := run(context.Background(), testCase.arguments, &stdout, &stderr, func(context.Context, string, ...string) ([]byte, error) {
				return []byte(testCase.output), testCase.err
			})
			if code != testCase.wantCode {
				t.Fatalf("run() = %d, want %d", code, testCase.wantCode)
			}
			if stdout.String() != testCase.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), testCase.wantStdout)
			}
			if stderr.String() != testCase.wantStderr {
				t.Fatalf("stderr = %q, want %q", stderr.String(), testCase.wantStderr)
			}
		})
	}
}

func TestRunUsesBackgroundContextForNil(t *testing.T) {
	var received context.Context
	code := run(nil, nil, &bytes.Buffer{}, &bytes.Buffer{}, func(ctx context.Context, executable string, arguments ...string) ([]byte, error) {
		received = ctx
		if executable != "go" || strings.Join(arguments, " ") != "test -count=1 -cover ./..." {
			t.Fatalf("command = %s %v", executable, arguments)
		}
		return []byte("coverage: 100.0% of statements\n"), nil
	})
	if code != 0 || received == nil {
		t.Fatalf("run() = %d, context = %v", code, received)
	}
}

func TestOutputParsersAndRunGoCommand(t *testing.T) {
	output := "a [no test files]\r\nb coverage: 99.9% of statements\r\nc coverage: 100.0% of statements\r\n"
	if got := packagesWithoutTests(output); len(got) != 1 || got[0] != "a [no test files]" {
		t.Fatalf("packagesWithoutTests() = %#v", got)
	}
	if got := incompletePackages(output); len(got) != 1 || got[0] != "b coverage: 99.9% of statements" {
		t.Fatalf("incompletePackages() = %#v", got)
	}
	if got := incompletePackages("coverage: [no statements]\ncoverage:"); len(got) != 0 {
		t.Fatalf("incompletePackages(no statements) = %#v", got)
	}

	outputBytes, err := runGoCommand(context.Background(), "go", "version")
	if err != nil || !strings.Contains(string(outputBytes), "go version") {
		t.Fatalf("runGoCommand() = %q, %v", outputBytes, err)
	}
}

func TestMainUsesConfiguredDependencies(t *testing.T) {
	originalExit := exitProcess
	originalArgs := commandArgs
	originalRun := runCommand
	defer func() {
		exitProcess = originalExit
		commandArgs = originalArgs
		runCommand = originalRun
	}()

	exitCode := -1
	exitProcess = func(code int) { exitCode = code }
	commandArgs = []string{"check-coverage"}
	runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("coverage: 100.0% of statements\n"), nil
	}

	main()
	if exitCode != 0 {
		t.Fatalf("main() exit code = %d", exitCode)
	}
}
