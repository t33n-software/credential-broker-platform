package packaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
			"persist-credentials: false",
			"go run -mod=readonly ./cmd/build",
			"FuzzParseRepository",
			"FuzzTokenRequestBoundary",
		},
	}
	assertWorkflowContract(t, testCase.path, testCase.required)
	assertRepositoryFileDoesNotContain(t, testCase.path, []string{
		"code-quality: write",
		"cmd/coverage-cobertura",
		"actions/upload-code-coverage",
		"coverage.xml",
		"code-coverage/go",
	})

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
			"t33n-software/git-governance",
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
	})
	assertRepositoryFileDoesNotContain(t, "docs/hosting-platforms/github/rulesets/README.md", []string{
		"GitHub Code Quality",
		"GitHub Code Coverage",
		"Cobertura XML",
		"code-quality: write",
	})
}

func TestRulesetSourcesAreCompleteAndPortable(t *testing.T) {
	rulesetDirectory := filepath.Join("..", "..", "docs", "hosting-platforms", "github", "rulesets")
	entries, err := os.ReadDir(rulesetDirectory)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", rulesetDirectory, err)
	}

	jsonNames := make([]string, 0)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			jsonNames = append(jsonNames, entry.Name())
		}
	}
	sort.Strings(jsonNames)
	wantNames := []string{
		"00-push-protections.json",
		"01-ticket-working-branches.json",
		"02-develop.json",
		"03-main.json",
	}
	if !reflect.DeepEqual(jsonNames, wantNames) {
		t.Fatalf("ruleset JSON files = %#v, want %#v", jsonNames, wantNames)
	}

	for _, name := range jsonNames {
		content := readRepositoryFile(t, filepath.Join("docs", "hosting-platforms", "github", "rulesets", name))
		for _, forbidden := range []string{"code_quality", "code_coverage"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains unsupported %q", name, forbidden)
			}
		}
		if !strings.Contains(content, "\"bypass_actors\": []") {
			t.Fatalf("%s does not prohibit Ruleset bypass actors", name)
		}
	}
}

func TestPushProtectionsRulesetBlocksCredentialShapedArtifacts(t *testing.T) {
	push := readRepositoryFile(t, "docs/hosting-platforms/github/rulesets/00-push-protections.json")
	for _, required := range []string{
		"\"name\": \"push-protections: block secret and key shaped artifacts\"",
		"\"target\": \"push\"",
		"\"source\": \"t33n-software/credential-broker-platform\"",
		"\"enforcement\": \"active\"",
		"\"conditions\": null",
		"\"bypass_actors\": []",
		"file_extension_restriction",
		"restricted_file_extensions",
		"file_path_restriction",
		"restricted_file_paths",
	} {
		if !strings.Contains(push, required) {
			t.Fatalf("00-push-protections.json does not contain %q", required)
		}
	}
	for _, extension := range []string{"pem", "key", "p12", "pfx", "jks", "keystore", "kdbx", "ppk", "gpg"} {
		if !strings.Contains(push, "\"*."+extension+"\"") {
			t.Fatalf("00-push-protections.json does not restrict the %q extension in glob form", extension)
		}
	}
	for _, path := range []string{"**/.env", "**/.env.*", "**/credentials", "**/credentials.*", "**/*.tfstate", "**/*.tfstate.*"} {
		if !strings.Contains(push, "\""+path+"\"") {
			t.Fatalf("00-push-protections.json does not restrict the %q path", path)
		}
	}
	for _, forbidden := range []string{"ref_name", "required_status_checks", "code_scanning", "code_quality", "code_coverage"} {
		if strings.Contains(push, forbidden) {
			t.Fatalf("00-push-protections.json unexpectedly contains %q; a push ruleset has no branch targets or check bindings", forbidden)
		}
	}

	readme := normalizeWhitespace(readRepositoryFile(t, "docs/hosting-platforms/github/rulesets/README.md"))
	for _, required := range []string{"00-push-protections.json", "fork network", "Team plan", "public"} {
		if !strings.Contains(readme, required) {
			t.Fatalf("Ruleset README does not document the push protections token %q", required)
		}
	}
}

func TestModuleIdentityMatchesOrganizationNamespace(t *testing.T) {
	goMod := readRepositoryFile(t, "go.mod")
	for _, required := range []string{
		"module github.com/t33n-software/credential-broker-platform",
		"go 1.26",
		"toolchain go1.26.5",
	} {
		if !strings.Contains(goMod, required) {
			t.Fatalf("go.mod does not contain %q", required)
		}
	}
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
	assertRuleTypes(t, ruleset, "deletion", "non_fast_forward", "pull_request", "required_status_checks", "code_scanning")
	assertNoRuleTypes(t, ruleset, "code_quality", "code_coverage")

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

func assertNoRuleTypes(t *testing.T, ruleset importableRuleset, forbidden ...string) {
	t.Helper()
	for _, ruleType := range forbidden {
		if containsRule(ruleset, ruleType) {
			t.Fatalf("Ruleset contains forbidden rule type %q", ruleType)
		}
	}
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

func assertRepositoryFileDoesNotContain(t *testing.T, path string, forbidden []string) {
	t.Helper()
	contents := readRepositoryFile(t, path)
	for _, value := range forbidden {
		if strings.Contains(contents, value) {
			t.Fatalf("%s contains forbidden value %q", path, value)
		}
	}
}

func normalizeWhitespace(content string) string {
	return strings.Join(strings.Fields(content), " ")
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(contents), "\r\n", "\n")
}
