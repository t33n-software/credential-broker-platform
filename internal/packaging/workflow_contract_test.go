package packaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformWorkflowContracts(t *testing.T) {
	testCase := struct {
		path     string
		required []string
	}{
		path: ".github/workflows/ci.yml",
		required: []string{
			"name: CI",
			"branches:",
			"      - main",
			"      - develop",
			"permissions:\n  contents: read",
			"code-quality: write",
			"persist-credentials: false",
			"go run -mod=readonly ./cmd/build",
			"go run -mod=readonly ./cmd/coverage-cobertura",
			"actions/upload-code-coverage@1c15be36fc3733ba839b1dd643bd9556e4426dc1",
			"file: coverage.xml",
			"language: Go",
			"label: code-coverage/go",
			"FuzzParseRepository",
			"FuzzTokenRequestBoundary",
		},
	}
	assertWorkflowContract(t, testCase.path, testCase.required)

	assertWorkflowContract(t, ".github/workflows/codeql.yml", []string{
		"name: CodeQL",
		"security-events: write",
		"languages: go",
		"go build -mod=readonly ./...",
	})
	assertWorkflowContract(t, ".github/workflows/dependency-review.yml", []string{
		"name: Dependency Review",
		"fail-on-severity: low",
		"fail-on-scopes: runtime,development,unknown",
	})
	assertWorkflowContract(t, ".github/dependabot.yml", []string{
		"package-ecosystem: gomod",
		"package-ecosystem: github-actions",
		"package-ecosystem: docker",
		"target-branch: develop",
	})

	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/codeql.yml",
		".github/workflows/dependency-review.yml",
	} {
		contents := readRepositoryFile(t, path)
		for _, forbidden := range []string{
			"CyberT33N/git-governance",
			"git-governance-release-broker",
			"GCP_",
			"BROKER_APP_ID",
			"BROKER_PRIVATE_KEY_PATH",
			"secrets.",
		} {
			if strings.Contains(contents, forbidden) {
				t.Fatalf("%s contains tenant-specific or secret-bearing value %q", path, forbidden)
			}
		}
	}
}

func TestQualityGateContract(t *testing.T) {
	var quality struct {
		SchemaVersion int `json:"schemaVersion"`
		Gates         []struct {
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"gates"`
	}
	if err := json.Unmarshal([]byte(readRepositoryFile(t, "git-governance.quality.json")), &quality); err != nil {
		t.Fatalf("decode quality configuration: %v", err)
	}
	if quality.SchemaVersion != 2 {
		t.Fatalf("schemaVersion = %d, want 2", quality.SchemaVersion)
	}

	want := map[string]string{
		"platform-source-quality": "go run -mod=readonly ./cmd/build",
	}
	if len(quality.Gates) != len(want) {
		t.Fatalf("gate count = %d, want %d", len(quality.Gates), len(want))
	}
	for _, gate := range quality.Gates {
		actual := strings.TrimSpace(gate.Command + " " + strings.Join(gate.Args, " "))
		if expected, ok := want[gate.Name]; !ok || actual != expected {
			t.Fatalf("gate %q = %q, want one of %#v", gate.Name, actual, want)
		}
		delete(want, gate.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing required gates: %#v", want)
	}
}

func TestLocalFortressContracts(t *testing.T) {
	assertWorkflowContract(t, "lefthook.yml", []string{
		"pre-push:",
		"parallel: false",
		"platform-source-quality:",
		"go run -mod=readonly ./cmd/build",
	})
	assertWorkflowContract(t, "Dockerfile", []string{
		"ARG BUILDER_IMAGE",
		"FROM --platform=linux/amd64 ${BUILDER_IMAGE} AS build",
		"CGO_ENABLED=0 GOOS=linux GOARCH=amd64",
		"FROM scratch",
		"USER 65532:65532",
		"ENTRYPOINT [\"/credential-broker\"]",
	})
	assertWorkflowContract(t, ".dockerignore", []string{
		".git",
		".github",
		"*.pem",
		"*.key",
		"*.crt",
	})
	assertWorkflowContract(t, "docs/development/VERIFICATION.md", []string{
		"go run -mod=readonly ./cmd/build",
		"approved internal Go proxy",
		"evidence-verified internal Go 1.26.5 builder artifact",
		"Tenant App keys",
	})
}

func TestPlatformRulesetContracts(t *testing.T) {
	ticket := loadRuleset(t, "docs/hosting-platforms/github/rulesets/01-ticket-working-branches.json")
	if ticket.Name != "credential-broker-platform: official ticket and hotfix working branches" {
		t.Fatalf("ticket Ruleset name = %q", ticket.Name)
	}
	if ticket.Target != "branch" || ticket.Enforcement != "active" || len(ticket.BypassActors) != 0 {
		t.Fatalf("ticket Ruleset boundary = %#v", ticket)
	}
	if strings.Join(ticket.Conditions.RefName.Include, ",") != strings.Join([]string{
		"refs/heads/feature/*",
		"refs/heads/fix/*",
		"refs/heads/docs/*",
		"refs/heads/refactor/*",
		"refs/heads/chore/*",
		"refs/heads/test/*",
		"refs/heads/perf/*",
		"refs/heads/hotfix/*",
	}, ",") {
		t.Fatalf("ticket Ruleset branch patterns = %#v", ticket.Conditions.RefName.Include)
	}
	assertRuleTypes(t, ticket, "non_fast_forward")

	develop := loadRuleset(t, "docs/hosting-platforms/github/rulesets/02-develop.json")
	assertSharedRuleset(t, develop, "credential-broker-platform: develop shared line", "refs/heads/develop", []string{"merge", "rebase", "squash"})

	main := loadRuleset(t, "docs/hosting-platforms/github/rulesets/03-main.json")
	assertSharedRuleset(t, main, "credential-broker-platform: main shared line", "refs/heads/main", []string{"merge"})

	assertWorkflowContract(t, "docs/hosting-platforms/github/rulesets/README.md", []string{
		"Always suggest updating pull request branches: disabled",
		"Enable release immutability: enabled",
		"Quality gates (linux-amd64)",
		"Dependency admission review",
		"GitHub Code Quality",
		"GitHub Code Coverage",
		"Cobertura XML",
	})
}

type importableRuleset struct {
	Name         string            `json:"name"`
	Target       string            `json:"target"`
	Enforcement  string            `json:"enforcement"`
	BypassActors []json.RawMessage `json:"bypass_actors"`
	Conditions   struct {
		RefName struct {
			Include []string `json:"include"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []rulesetRule `json:"rules"`
}

type rulesetRule struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}

func loadRuleset(t *testing.T, path string) importableRuleset {
	t.Helper()
	var ruleset importableRuleset
	if err := json.Unmarshal([]byte(readRepositoryFile(t, path)), &ruleset); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return ruleset
}

func assertSharedRuleset(t *testing.T, ruleset importableRuleset, name string, ref string, mergeMethods []string) {
	t.Helper()
	if ruleset.Name != name || ruleset.Target != "branch" || ruleset.Enforcement != "active" || len(ruleset.BypassActors) != 0 {
		t.Fatalf("shared Ruleset boundary = %#v", ruleset)
	}
	if strings.Join(ruleset.Conditions.RefName.Include, ",") != ref {
		t.Fatalf("shared Ruleset ref patterns = %#v, want %q", ruleset.Conditions.RefName.Include, ref)
	}
	assertRuleTypes(t, ruleset, "deletion", "non_fast_forward", "pull_request", "required_status_checks", "code_scanning", "code_quality", "code_coverage")

	var pullRequest struct {
		RequiredApprovingReviewCount   int      `json:"required_approving_review_count"`
		DismissStaleReviewsOnPush      bool     `json:"dismiss_stale_reviews_on_push"`
		RequireLastPushApproval        bool     `json:"require_last_push_approval"`
		RequiredReviewThreadResolution bool     `json:"required_review_thread_resolution"`
		AllowedMergeMethods            []string `json:"allowed_merge_methods"`
	}
	decodeRuleParameters(t, ruleset, "pull_request", &pullRequest)
	if pullRequest.RequiredApprovingReviewCount != 1 || !pullRequest.DismissStaleReviewsOnPush || pullRequest.RequireLastPushApproval || !pullRequest.RequiredReviewThreadResolution || strings.Join(pullRequest.AllowedMergeMethods, ",") != strings.Join(mergeMethods, ",") {
		t.Fatalf("pull request parameters = %#v", pullRequest)
	}

	var statusChecks struct {
		StrictRequiredStatusChecksPolicy bool `json:"strict_required_status_checks_policy"`
		RequiredStatusChecks             []struct {
			Context string `json:"context"`
		} `json:"required_status_checks"`
	}
	decodeRuleParameters(t, ruleset, "required_status_checks", &statusChecks)
	if !statusChecks.StrictRequiredStatusChecksPolicy {
		t.Fatal("strict required status checks = false")
	}
	contexts := make([]string, 0, len(statusChecks.RequiredStatusChecks))
	for _, check := range statusChecks.RequiredStatusChecks {
		contexts = append(contexts, check.Context)
	}
	if strings.Join(contexts, ",") != "Quality gates (linux-amd64),Dependency admission review" {
		t.Fatalf("required status contexts = %#v", contexts)
	}

	var codeScanning struct {
		Tools []struct {
			Tool                    string `json:"tool"`
			AlertsThreshold         string `json:"alerts_threshold"`
			SecurityAlertsThreshold string `json:"security_alerts_threshold"`
		} `json:"code_scanning_tools"`
	}
	decodeRuleParameters(t, ruleset, "code_scanning", &codeScanning)
	if len(codeScanning.Tools) != 1 || codeScanning.Tools[0].Tool != "CodeQL" || codeScanning.Tools[0].AlertsThreshold != "all" || codeScanning.Tools[0].SecurityAlertsThreshold != "all" {
		t.Fatalf("code scanning parameters = %#v", codeScanning)
	}

	var codeQuality struct {
		Severity string `json:"severity"`
	}
	decodeRuleParameters(t, ruleset, "code_quality", &codeQuality)
	if codeQuality.Severity != "all" {
		t.Fatalf("code quality parameters = %#v", codeQuality)
	}

	var codeCoverage struct {
		MinimumCoverage int `json:"minimum_coverage"`
		MaxCoverageDrop int `json:"max_coverage_drop"`
	}
	decodeRuleParameters(t, ruleset, "code_coverage", &codeCoverage)
	if codeCoverage.MinimumCoverage != 100 || codeCoverage.MaxCoverageDrop != 0 {
		t.Fatalf("code coverage parameters = %#v", codeCoverage)
	}
}

func assertRuleTypes(t *testing.T, ruleset importableRuleset, want ...string) {
	t.Helper()
	got := make([]string, 0, len(ruleset.Rules))
	for _, rule := range ruleset.Rules {
		got = append(got, rule.Type)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Ruleset rule types = %#v, want %#v", got, want)
	}
}

func containsRule(ruleset importableRuleset, ruleType string) bool {
	for _, rule := range ruleset.Rules {
		if rule.Type == ruleType {
			return true
		}
	}
	return false
}

func decodeRuleParameters(t *testing.T, ruleset importableRuleset, ruleType string, target any) {
	t.Helper()
	for _, rule := range ruleset.Rules {
		if rule.Type != ruleType {
			continue
		}
		if err := json.Unmarshal(rule.Parameters, target); err != nil {
			t.Fatalf("decode %s parameters: %v", ruleType, err)
		}
		return
	}
	t.Fatalf("missing %s rule", ruleType)
}

func assertWorkflowContract(t *testing.T, path string, required []string) {
	t.Helper()
	contents := readRepositoryFile(t, path)
	for _, value := range required {
		if !strings.Contains(contents, value) {
			t.Fatalf("%s does not contain %q", path, value)
		}
	}
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(contents), "\r\n", "\n")
}
