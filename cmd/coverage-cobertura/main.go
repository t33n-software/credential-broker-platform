// Command coverage-cobertura converts an aggregate Go coverage profile into
// the Cobertura XML required by GitHub Code Coverage.
package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type sourceReader func(string) ([]byte, error)
type reportWriter func(string, []byte, os.FileMode) error
type directoryCreator func(string, os.FileMode) error

var (
	exitProcess     = os.Exit
	commandArgs     = os.Args
	readSource      = os.ReadFile
	writeReport     = os.WriteFile
	createDirectory = os.MkdirAll
	marshalReport   = xml.MarshalIndent
	now             = time.Now
)

type profileBlock struct {
	File       string
	StartLine  int
	EndLine    int
	Statements int
	Count      int
}

type coverageReport struct {
	XMLName         xml.Name          `xml:"coverage"`
	LineRate        string            `xml:"line-rate,attr"`
	BranchRate      string            `xml:"branch-rate,attr"`
	LinesCovered    int               `xml:"lines-covered,attr"`
	LinesValid      int               `xml:"lines-valid,attr"`
	BranchesCovered int               `xml:"branches-covered,attr"`
	BranchesValid   int               `xml:"branches-valid,attr"`
	Complexity      string            `xml:"complexity,attr"`
	Version         string            `xml:"version,attr"`
	Timestamp       int64             `xml:"timestamp,attr"`
	Sources         []string          `xml:"sources>source"`
	Packages        []coveragePackage `xml:"packages>package"`
}

type coveragePackage struct {
	Name       string          `xml:"name,attr"`
	LineRate   string          `xml:"line-rate,attr"`
	BranchRate string          `xml:"branch-rate,attr"`
	Complexity string          `xml:"complexity,attr"`
	Classes    []coverageClass `xml:"classes>class"`
}

type coverageClass struct {
	Name       string         `xml:"name,attr"`
	Filename   string         `xml:"filename,attr"`
	LineRate   string         `xml:"line-rate,attr"`
	BranchRate string         `xml:"branch-rate,attr"`
	Complexity string         `xml:"complexity,attr"`
	Methods    struct{}       `xml:"methods"`
	Lines      []coverageLine `xml:"lines>line"`
}

type coverageLine struct {
	Number int `xml:"number,attr"`
	Hits   int `xml:"hits,attr"`
}

func main() {
	exitProcess(run(commandArgs[1:], os.Stdout, os.Stderr, readSource, writeReport, createDirectory, now))
}

func run(arguments []string, stdout io.Writer, stderr io.Writer, read sourceReader, write reportWriter, makeDirectory directoryCreator, clock func() time.Time) int {
	flags := flag.NewFlagSet("coverage-cobertura", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "aggregate Go coverage profile")
	output := flags.String("output", "", "Cobertura XML output")
	module := flags.String("module", "", "Go module path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*input) == "" || strings.TrimSpace(*output) == "" || strings.TrimSpace(*module) == "" {
		fmt.Fprintln(stderr, "usage: coverage-cobertura --input <coverage.out> --output <coverage.xml> --module <module-path>")
		return 2
	}
	if clock == nil {
		clock = time.Now
	}

	profile, err := read(*input)
	if err != nil {
		fmt.Fprintf(stderr, "read coverage profile: %v\n", err)
		return 1
	}
	blocks, err := parseProfile(profile)
	if err != nil {
		fmt.Fprintf(stderr, "parse coverage profile: %v\n", err)
		return 1
	}
	report := buildCoverageReport(blocks, *module, clock().UTC())
	encoded, err := marshalReport(report, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode Cobertura report: %v\n", err)
		return 1
	}
	if err := makeDirectory(filepath.Dir(*output), 0o755); err != nil {
		fmt.Fprintf(stderr, "create output directory: %v\n", err)
		return 1
	}
	if err := write(*output, append([]byte(xml.Header), append(encoded, '\n')...), 0o644); err != nil {
		fmt.Fprintf(stderr, "write Cobertura report: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote Cobertura coverage report to %s.\n", *output)
	return 0
}

func parseProfile(contents []byte) ([]profileBlock, error) {
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "mode: ") {
		return nil, errors.New("missing coverage mode header")
	}
	switch strings.TrimSpace(strings.TrimPrefix(lines[0], "mode: ")) {
	case "set", "count", "atomic":
	default:
		return nil, errors.New("unsupported coverage mode")
	}

	blocks := make([]profileBlock, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		block, err := parseProfileBlock(line)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return nil, errors.New("coverage profile contains no blocks")
	}
	return blocks, nil
}

func parseProfileBlock(line string) (profileBlock, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return profileBlock{}, errors.New("coverage block must contain location, statements, and count")
	}
	locationSeparator := strings.LastIndex(fields[0], ":")
	if locationSeparator <= 0 {
		return profileBlock{}, errors.New("coverage block is missing a file location")
	}
	file := filepath.ToSlash(fields[0][:locationSeparator])
	positions := strings.Split(fields[0][locationSeparator+1:], ",")
	if len(positions) != 2 {
		return profileBlock{}, errors.New("coverage block has invalid positions")
	}
	startLine, err := parseLineNumber(positions[0])
	if err != nil {
		return profileBlock{}, err
	}
	endLine, err := parseLineNumber(positions[1])
	if err != nil {
		return profileBlock{}, err
	}
	if endLine < startLine {
		return profileBlock{}, errors.New("coverage block ends before it starts")
	}
	statements, err := strconv.Atoi(fields[1])
	if err != nil || statements < 0 {
		return profileBlock{}, errors.New("coverage block has invalid statement count")
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil || count < 0 {
		return profileBlock{}, errors.New("coverage block has invalid hit count")
	}
	return profileBlock{File: file, StartLine: startLine, EndLine: endLine, Statements: statements, Count: count}, nil
}

func parseLineNumber(position string) (int, error) {
	line, _, found := strings.Cut(position, ".")
	if !found {
		return 0, errors.New("coverage position is missing a column")
	}
	value, err := strconv.Atoi(line)
	if err != nil || value <= 0 {
		return 0, errors.New("coverage position has invalid line number")
	}
	return value, nil
}

func buildCoverageReport(blocks []profileBlock, module string, timestamp time.Time) coverageReport {
	files := make(map[string]map[int]int)
	for _, block := range blocks {
		if block.Statements == 0 {
			continue
		}
		filename := normalizeFilename(block.File, module)
		lines := files[filename]
		if lines == nil {
			lines = make(map[int]int)
			files[filename] = lines
		}
		for line := block.StartLine; line <= block.EndLine; line++ {
			current, found := lines[line]
			if !found || block.Count > current {
				lines[line] = block.Count
			}
		}
	}

	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	packageClasses := make(map[string][]coverageClass)
	packageTotals := make(map[string]lineTotals)
	var totals lineTotals
	for _, filename := range filenames {
		lines := sortedCoverageLines(files[filename])
		classTotals := totalsFor(lines)
		totals.add(classTotals)
		packageName := path.Dir(filename)
		if packageName == "." {
			packageName = ""
		}
		packageTotals[packageName] = packageTotals[packageName].plus(classTotals)
		packageClasses[packageName] = append(packageClasses[packageName], coverageClass{
			Name:       path.Base(filename),
			Filename:   filename,
			LineRate:   classTotals.rate(),
			BranchRate: "0.000000",
			Complexity: "0",
			Lines:      lines,
		})
	}

	packageNames := make([]string, 0, len(packageClasses))
	for packageName := range packageClasses {
		packageNames = append(packageNames, packageName)
	}
	sort.Strings(packageNames)
	packages := make([]coveragePackage, 0, len(packageNames))
	for _, packageName := range packageNames {
		packageTotal := packageTotals[packageName]
		packages = append(packages, coveragePackage{
			Name:       packageName,
			LineRate:   packageTotal.rate(),
			BranchRate: "0.000000",
			Complexity: "0",
			Classes:    packageClasses[packageName],
		})
	}

	return coverageReport{
		LineRate:        totals.rate(),
		BranchRate:      "0.000000",
		LinesCovered:    totals.covered,
		LinesValid:      totals.valid,
		BranchesCovered: 0,
		BranchesValid:   0,
		Complexity:      "0",
		Version:         "1.0",
		Timestamp:       timestamp.Unix(),
		Sources:         []string{"."},
		Packages:        packages,
	}
}

type lineTotals struct {
	covered int
	valid   int
}

func (totals *lineTotals) add(other lineTotals) {
	totals.covered += other.covered
	totals.valid += other.valid
}

func (totals lineTotals) plus(other lineTotals) lineTotals {
	return lineTotals{covered: totals.covered + other.covered, valid: totals.valid + other.valid}
}

func (totals lineTotals) rate() string {
	if totals.valid == 0 {
		return "0.000000"
	}
	return fmt.Sprintf("%.6f", float64(totals.covered)/float64(totals.valid))
}

func normalizeFilename(filename string, module string) string {
	filename = strings.TrimPrefix(filepath.ToSlash(filename), "./")
	module = strings.TrimSuffix(strings.TrimSpace(module), "/")
	if prefix := module + "/"; strings.HasPrefix(filename, prefix) {
		return strings.TrimPrefix(filename, prefix)
	}
	return filename
}

func sortedCoverageLines(lines map[int]int) []coverageLine {
	numbers := make([]int, 0, len(lines))
	for number := range lines {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	result := make([]coverageLine, 0, len(numbers))
	for _, number := range numbers {
		result = append(result, coverageLine{Number: number, Hits: lines[number]})
	}
	return result
}

func totalsFor(lines []coverageLine) lineTotals {
	totals := lineTotals{valid: len(lines)}
	for _, line := range lines {
		if line.Hits > 0 {
			totals.covered++
		}
	}
	return totals
}
