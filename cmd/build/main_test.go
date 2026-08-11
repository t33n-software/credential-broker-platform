package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	goFiles := func(string) ([]string, error) { return []string{"a.go"}, nil }
	success := func(context.Context, []string, string, ...string) ([]byte, error) { return nil, nil }
	noopDirectory := func(string, os.FileMode) error { return nil }
	readSource := func(string) ([]byte, error) { return []byte("package main\n"), nil }
	formatSource := func(source []byte) ([]byte, error) { return source, nil }

	for _, testCase := range []struct {
		name       string
		arguments  []string
		execute    commandRunner
		locate     goFileFinder
		read       sourceReader
		format     sourceFormatter
		makeDir    directoryCreator
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "arguments rejected",
			arguments:  []string{"unexpected"},
			execute:    success,
			locate:     goFiles,
			makeDir:    noopDirectory,
			wantCode:   2,
			wantStderr: "usage: build\n",
		},
		{
			name:    "find Go files fails",
			execute: success,
			locate: func(string) ([]string, error) {
				return nil, errors.New("find")
			},
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStderr: "list Go files: find\n",
		},
		{
			name:       "read source fails",
			execute:    success,
			locate:     goFiles,
			read:       func(string) ([]byte, error) { return nil, errors.New("read") },
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStdout: "==> check Go formatting\n",
			wantStderr: "read Go source: read\n",
		},
		{
			name:       "format source fails",
			execute:    success,
			locate:     goFiles,
			read:       readSource,
			format:     func([]byte) ([]byte, error) { return nil, errors.New("format") },
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStdout: "==> check Go formatting\n",
			wantStderr: "format Go source: format\n",
		},
		{
			name:       "format source reports file",
			execute:    success,
			locate:     goFiles,
			read:       func(string) ([]byte, error) { return []byte("package main\nfunc main(){}"), nil },
			format:     func([]byte) ([]byte, error) { return []byte("package main\n\nfunc main() {}\n"), nil },
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStdout: "==> check Go formatting\n",
			wantStderr: "the following files require gofmt:\na.go\n",
		},
		{
			name: "source quality step fails",
			execute: func(_ context.Context, _ []string, executable string, arguments ...string) ([]byte, error) {
				if executable == "go" && reflect.DeepEqual(arguments, []string{"mod", "verify"}) {
					return []byte("module output"), errors.New("verify")
				}
				return nil, nil
			},
			locate:     goFiles,
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStdout: "==> check Go formatting\n==> verify module checksums\nmodule output",
			wantStderr: "verify module checksums: verify\n",
		},
		{
			name:       "build directory fails",
			execute:    success,
			locate:     goFiles,
			makeDir:    func(string, os.FileMode) error { return errors.New("directory") },
			wantCode:   1,
			wantStdout: "==> check Go formatting\n==> verify module checksums\n==> verify module metadata\n==> run unit tests\n==> enforce complete statement coverage\n==> run race detector\n==> run static analysis\n",
			wantStderr: "create build directory: directory\n",
		},
		{
			name: "Linux build fails",
			execute: func(_ context.Context, _ []string, executable string, arguments ...string) ([]byte, error) {
				if executable == "go" && len(arguments) > 0 && arguments[0] == "build" {
					return []byte("build output"), errors.New("build")
				}
				return nil, nil
			},
			locate:     goFiles,
			makeDir:    noopDirectory,
			wantCode:   1,
			wantStdout: "==> check Go formatting\n==> verify module checksums\n==> verify module metadata\n==> run unit tests\n==> enforce complete statement coverage\n==> run race detector\n==> run static analysis\n==> build Linux AMD64 credential broker\nbuild output",
			wantStderr: "build Linux AMD64 credential broker: build\n",
		},
		{
			name:       "success",
			execute:    success,
			locate:     goFiles,
			makeDir:    noopDirectory,
			wantCode:   0,
			wantStdout: "==> check Go formatting\n==> verify module checksums\n==> verify module metadata\n==> run unit tests\n==> enforce complete statement coverage\n==> run race detector\n==> run static analysis\n==> build Linux AMD64 credential broker\n==> record Linux credential broker module provenance\nCredential broker platform source-level build completed successfully.\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			read := testCase.read
			if read == nil {
				read = readSource
			}
			format := testCase.format
			if format == nil {
				format = formatSource
			}
			code := run(context.Background(), testCase.arguments, &stdout, &stderr, testCase.execute, testCase.locate, read, format, testCase.makeDir)
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

func TestRunUsesBackgroundContextAndLinuxTarget(t *testing.T) {
	var (
		receivedContext context.Context
		receivedEnv     []string
	)
	code := run(nil, nil, &bytes.Buffer{}, &bytes.Buffer{}, func(ctx context.Context, environment []string, executable string, arguments ...string) ([]byte, error) {
		receivedContext = ctx
		if executable == "go" && len(arguments) > 0 && arguments[0] == "build" {
			receivedEnv = environment
		}
		return nil, nil
	}, func(string) ([]string, error) {
		return []string{"a.go"}, nil
	}, func(string) ([]byte, error) {
		return []byte("package main\n"), nil
	}, func(source []byte) ([]byte, error) {
		return source, nil
	}, func(string, os.FileMode) error { return nil })
	if code != 0 || receivedContext == nil {
		t.Fatalf("run() = %d, context = %v", code, receivedContext)
	}
	if !reflect.DeepEqual(receivedEnv, []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64"}) {
		t.Fatalf("Linux build environment = %#v", receivedEnv)
	}
}

func TestStepsAndFormatting(t *testing.T) {
	if len(sourceQualitySteps()) != 6 {
		t.Fatalf("sourceQualitySteps() length = %d", len(sourceQualitySteps()))
	}
	if got := linuxBuildSteps(); len(got) != 2 || got[0].arguments[4] != linuxBrokerPath {
		t.Fatalf("linuxBuildSteps() = %#v", got)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if runSteps(context.Background(), nil, &stdout, &stderr, func(context.Context, []string, string, ...string) ([]byte, error) {
		return nil, nil
	}) != true {
		t.Fatal("runSteps(empty) = false")
	}
	if runStep(context.Background(), step{name: "output", executable: "tool"}, &stdout, &stderr, func(context.Context, []string, string, ...string) ([]byte, error) {
		return []byte("output"), nil
	}) != true {
		t.Fatal("runStep(success) = false")
	}
	if !strings.Contains(stdout.String(), "==> output\noutput") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	if checkFormatting(&bytes.Buffer{}, &bytes.Buffer{}, func(string) ([]string, error) {
		return []string{"a.go"}, nil
	}, func(string) ([]byte, error) {
		return []byte("package main\r\n"), nil
	}, func(source []byte) ([]byte, error) {
		return []byte("package main\n"), nil
	}) != true {
		t.Fatal("checkFormatting() = false")
	}
	if got := normalizeLineEndings([]byte("a\r\nb\r\n")); string(got) != "a\nb\n" {
		t.Fatalf("normalizeLineEndings() = %q", got)
	}
}

func TestGoFilesAndHelpers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"main.go",
		filepath.Join("nested", "nested.go"),
		filepath.Join("vendor", "ignored.go"),
		filepath.Join(".build", "ignored.go"),
		"readme.md",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	got, err := goFiles(root)
	if err != nil {
		t.Fatalf("goFiles() error = %v", err)
	}
	want := []string{filepath.Join(root, "main.go"), filepath.Join(root, "nested", "nested.go")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goFiles() = %#v, want %#v", got, want)
	}
	if _, err := goFiles(filepath.Join(root, "missing")); err == nil {
		t.Fatal("goFiles(missing) error = nil")
	}
	for _, name := range []string{".build", ".git", ".cache", "coverage", "dist", "vendor"} {
		if !ignoredDirectory(name) {
			t.Fatalf("ignoredDirectory(%q) = false", name)
		}
	}
	if ignoredDirectory("source") {
		t.Fatal("ignoredDirectory(source) = true")
	}

	output, err := runCommand(context.Background(), []string{"GOV29_TEST_ENVIRONMENT=present"}, "go", "version")
	if err != nil || !strings.Contains(string(output), "go version") {
		t.Fatalf("runCommand() = %q, %v", output, err)
	}
}

func TestMainUsesConfiguredDependencies(t *testing.T) {
	originalExit := exitProcess
	originalArgs := commandArgs
	originalRun := runExternalCommand
	originalFiles := findGoFiles
	originalRead := readSource
	originalFormat := formatSource
	originalDirectory := createDirectory
	defer func() {
		exitProcess = originalExit
		commandArgs = originalArgs
		runExternalCommand = originalRun
		findGoFiles = originalFiles
		readSource = originalRead
		formatSource = originalFormat
		createDirectory = originalDirectory
	}()

	exitCode := -1
	exitProcess = func(code int) { exitCode = code }
	commandArgs = []string{"build"}
	runExternalCommand = func(context.Context, []string, string, ...string) ([]byte, error) {
		return nil, nil
	}
	findGoFiles = func(string) ([]string, error) { return []string{"a.go"}, nil }
	readSource = func(string) ([]byte, error) { return []byte("package main\n"), nil }
	formatSource = func(source []byte) ([]byte, error) { return source, nil }
	createDirectory = func(string, os.FileMode) error { return nil }

	main()
	if exitCode != 0 {
		t.Fatalf("main() exit code = %d", exitCode)
	}
}
