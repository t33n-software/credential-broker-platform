package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	profile := []byte("mode: atomic\nexample.com/platform/cmd/main.go:1.1,2.2 1 3\n")
	successRead := func(string) ([]byte, error) { return profile, nil }
	successWrite := func(string, []byte, os.FileMode) error { return nil }
	successDirectory := func(string, os.FileMode) error { return nil }

	for _, testCase := range []struct {
		name       string
		arguments  []string
		read       sourceReader
		write      reportWriter
		makeDir    directoryCreator
		now        func() time.Time
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "missing arguments",
			wantCode:   2,
			wantStderr: "usage: coverage-cobertura --input <coverage.out> --output <coverage.xml> --module <module-path>\n",
		},
		{
			name:       "unknown flag",
			arguments:  []string{"--unknown"},
			wantCode:   2,
			wantStderr: "flag provided but not defined: -unknown\nUsage of coverage-cobertura:\n  -input string\n    \taggregate Go coverage profile\n  -module string\n    \tGo module path\n  -output string\n    \tCobertura XML output\n",
		},
		{
			name:      "read failure",
			arguments: []string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
			read: func(string) ([]byte, error) {
				return nil, errors.New("read")
			},
			wantCode:   1,
			wantStderr: "read coverage profile: read\n",
		},
		{
			name:      "profile failure",
			arguments: []string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
			read: func(string) ([]byte, error) {
				return []byte("invalid"), nil
			},
			wantCode:   1,
			wantStderr: "parse coverage profile: missing coverage mode header\n",
		},
		{
			name:      "directory failure",
			arguments: []string{"--input", "coverage.out", "--output", "out/coverage.xml", "--module", "example.com/platform"},
			read:      successRead,
			makeDir: func(string, os.FileMode) error {
				return errors.New("directory")
			},
			wantCode:   1,
			wantStderr: "create output directory: directory\n",
		},
		{
			name:      "write failure",
			arguments: []string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
			read:      successRead,
			write: func(string, []byte, os.FileMode) error {
				return errors.New("write")
			},
			makeDir:    successDirectory,
			wantCode:   1,
			wantStderr: "write Cobertura report: write\n",
		},
		{
			name:       "nil clock",
			arguments:  []string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
			read:       successRead,
			write:      successWrite,
			makeDir:    successDirectory,
			wantCode:   0,
			wantStdout: "Wrote Cobertura coverage report to coverage.xml.\n",
		},
		{
			name:      "success",
			arguments: []string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
			read:      successRead,
			write:     successWrite,
			makeDir:   successDirectory,
			now: func() time.Time {
				return time.Unix(123, 0)
			},
			wantCode:   0,
			wantStdout: "Wrote Cobertura coverage report to coverage.xml.\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			read := testCase.read
			if read == nil {
				read = successRead
			}
			write := testCase.write
			if write == nil {
				write = successWrite
			}
			makeDir := testCase.makeDir
			if makeDir == nil {
				makeDir = successDirectory
			}
			code := run(testCase.arguments, &stdout, &stderr, read, write, makeDir, testCase.now)
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

func TestRunHandlesEncodingFailure(t *testing.T) {
	originalMarshal := marshalReport
	defer func() { marshalReport = originalMarshal }()
	marshalReport = func(any, string, string) ([]byte, error) {
		return nil, errors.New("encode")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(
		[]string{"--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"},
		&stdout,
		&stderr,
		func(string) ([]byte, error) {
			return []byte("mode: set\nexample.com/platform/main.go:1.1,1.2 1 1\n"), nil
		},
		func(string, []byte, os.FileMode) error { return nil },
		func(string, os.FileMode) error { return nil },
		time.Now,
	)
	if code != 1 || stdout.Len() != 0 || stderr.String() != "encode Cobertura report: encode\n" {
		t.Fatalf("run() = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunWritesCoberturaXML(t *testing.T) {
	var (
		writtenPath string
		written     []byte
		directory   string
	)
	code := run(
		[]string{"--input", "coverage.out", "--output", "out/coverage.xml", "--module", "example.com/platform"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) ([]byte, error) {
			return []byte("mode: set\nexample.com/platform/internal/issuer.go:2.1,2.2 1 1\n"), nil
		},
		func(path string, contents []byte, mode os.FileMode) error {
			writtenPath = path
			written = contents
			if mode != 0o644 {
				t.Fatalf("write mode = %o", mode)
			}
			return nil
		},
		func(path string, mode os.FileMode) error {
			directory = path
			if mode != 0o755 {
				t.Fatalf("directory mode = %o", mode)
			}
			return nil
		},
		func() time.Time { return time.Unix(456, 0) },
	)
	if code != 0 || writtenPath != "out/coverage.xml" || directory != "out" {
		t.Fatalf("run() = %d, path = %q, directory = %q", code, writtenPath, directory)
	}
	var report coverageReport
	if err := xml.Unmarshal(written, &report); err != nil {
		t.Fatalf("unmarshal XML: %v", err)
	}
	if report.LineRate != "1.000000" || report.LinesCovered != 1 || report.LinesValid != 1 || report.Timestamp != 456 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Packages) != 1 || report.Packages[0].Name != "internal" || len(report.Packages[0].Classes) != 1 || report.Packages[0].Classes[0].Filename != "internal/issuer.go" {
		t.Fatalf("packages = %#v", report.Packages)
	}
}

func TestProfileParsingAndReportConstruction(t *testing.T) {
	for _, contents := range [][]byte{
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1.2 1 1\n"),
		[]byte("mode: count\nexample.com/platform/main.go:1.1,1.2 1 1\n"),
		[]byte("mode: atomic\nexample.com/platform/main.go:1.1,1.2 1 1\n"),
		[]byte("mode: atomic\nexample.com/platform/main.go:1.1,1.2 0 1\n"),
		[]byte("mode: atomic\n\nexample.com/platform/main.go:1.1,1.2 1 1\n"),
	} {
		if _, err := parseProfile(contents); err != nil {
			t.Fatalf("parseProfile(%q) error = %v", contents, err)
		}
	}
	for _, contents := range [][]byte{
		nil,
		[]byte("mode: unsupported\nexample.com/platform/main.go:1.1,1.2 1 1\n"),
		[]byte("mode: set\n"),
		[]byte("mode: set\ninvalid\n"),
		[]byte("mode: set\n:1.1,1.2 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:line.1,1.2 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1,1.2 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:0.1,1.2 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1.2 -1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1.2 x 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:2.1,1.2 1 1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1.2 1 -1\n"),
		[]byte("mode: set\nexample.com/platform/main.go:1.1,1.2 1 x\n"),
	} {
		if _, err := parseProfile(contents); err == nil {
			t.Fatalf("parseProfile(%q) error = nil", contents)
		}
	}

	blocks, err := parseProfile([]byte(strings.Join([]string{
		"mode: atomic",
		"example.com/platform/internal/issuer.go:1.1,2.2 1 3",
		"example.com/platform/internal/issuer.go:2.1,3.2 1 0",
		"external/other.go:5.1,5.2 1 2",
	}, "\n")))
	if err != nil {
		t.Fatalf("parseProfile() error = %v", err)
	}
	report := buildCoverageReport(blocks, "example.com/platform", time.Unix(789, 0))
	if report.LineRate != "0.750000" || report.LinesCovered != 3 || report.LinesValid != 4 {
		t.Fatalf("report totals = %#v", report)
	}
	if len(report.Packages) != 2 || report.Packages[0].Name != "external" || report.Packages[1].Name != "internal" {
		t.Fatalf("report packages = %#v", report.Packages)
	}
	lines := report.Packages[1].Classes[0].Lines
	if !reflect.DeepEqual(lines, []coverageLine{{Number: 1, Hits: 3}, {Number: 2, Hits: 3}, {Number: 3, Hits: 0}}) {
		t.Fatalf("issuer lines = %#v", lines)
	}
}

func TestHelpers(t *testing.T) {
	if got := normalizeFilename("example.com/platform/internal/file.go", "example.com/platform"); got != "internal/file.go" {
		t.Fatalf("normalizeFilename() = %q", got)
	}
	if got := normalizeFilename("./external/file.go", "example.com/platform"); got != "external/file.go" {
		t.Fatalf("normalizeFilename() = %q", got)
	}
	if got := sortedCoverageLines(map[int]int{3: 0, 1: 2}); !reflect.DeepEqual(got, []coverageLine{{Number: 1, Hits: 2}, {Number: 3, Hits: 0}}) {
		t.Fatalf("sortedCoverageLines() = %#v", got)
	}
	totals := totalsFor([]coverageLine{{Number: 1, Hits: 1}, {Number: 2, Hits: 0}})
	totals.add(lineTotals{covered: 1, valid: 1})
	if totals != (lineTotals{covered: 2, valid: 3}) || totals.rate() != "0.666667" {
		t.Fatalf("totals = %#v, rate = %s", totals, totals.rate())
	}
	if got := (lineTotals{}).rate(); got != "0.000000" {
		t.Fatalf("empty rate = %q", got)
	}
	if got := (lineTotals{covered: 1, valid: 1}).plus(lineTotals{covered: 2, valid: 3}); got != (lineTotals{covered: 3, valid: 4}) {
		t.Fatalf("plus() = %#v", got)
	}
	if report := buildCoverageReport([]profileBlock{{File: "example.com/platform/generated.go", StartLine: 1, EndLine: 1, Statements: 0, Count: 99}}, "example.com/platform", time.Unix(1, 0)); report.LinesValid != 0 || len(report.Packages) != 0 {
		t.Fatalf("zero-statement profile block = %#v", report)
	}
}

func TestMainUsesConfiguredDependencies(t *testing.T) {
	originalExit := exitProcess
	originalArgs := commandArgs
	originalRead := readSource
	originalWrite := writeReport
	originalDirectory := createDirectory
	originalMarshal := marshalReport
	originalNow := now
	defer func() {
		exitProcess = originalExit
		commandArgs = originalArgs
		readSource = originalRead
		writeReport = originalWrite
		createDirectory = originalDirectory
		marshalReport = originalMarshal
		now = originalNow
	}()

	exitCode := -1
	exitProcess = func(code int) { exitCode = code }
	commandArgs = []string{"coverage-cobertura", "--input", "coverage.out", "--output", "coverage.xml", "--module", "example.com/platform"}
	readSource = func(string) ([]byte, error) {
		return []byte("mode: set\nexample.com/platform/main.go:1.1,1.2 1 1\n"), nil
	}
	writeReport = func(string, []byte, os.FileMode) error { return nil }
	createDirectory = func(string, os.FileMode) error { return nil }
	now = func() time.Time { return time.Unix(1, 0) }

	main()
	if exitCode != 0 {
		t.Fatalf("main() exit code = %d", exitCode)
	}
}
