package packaging

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bindingManifest mirrors the tenant binding manifest (repo-bindings/v1) for
// the self-consistency proofs of the canonical adoption. The home-side proof
// against the canonical masters is owned by the verify-canonical tool; these
// tests bind the tenant files to the manifest.
type bindingManifest struct {
	Home struct {
		Repository string `json:"repository"`
		SHA        string `json:"sha"`
	} `json:"home"`
	Callers []struct {
		File   string `json:"file"`
		Master string `json:"master"`
		SHA256 string `json:"sha256"`
	} `json:"callers"`
	Files struct {
		Lefthook      fileBinding `json:"lefthook"`
		Gitattributes fileBinding `json:"gitattributes"`
		Gitignore     fileBinding `json:"gitignore"`
		Dependabot    fileBinding `json:"dependabot"`
	} `json:"files"`
	Codeowners struct {
		Path         string `json:"path"`
		DefaultOwner string `json:"defaultOwner"`
	} `json:"codeowners"`
}

type fileBinding struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func readBindingManifest(t *testing.T) bindingManifest {
	t.Helper()
	var manifest bindingManifest
	if err := json.Unmarshal([]byte(readRepositoryFile(t, "repo-bindings.json")), &manifest); err != nil {
		t.Fatalf("repo-bindings.json is not valid JSON: %v", err)
	}
	if manifest.Home.Repository != "t33n-software/repository-governance" {
		t.Fatalf("the manifest binds home %q", manifest.Home.Repository)
	}
	return manifest
}

// hashRepositoryFile hashes the repository file; the canonical .gitattributes
// makes the checkout LF, and the read helper's CRLF normalization keeps the
// derivation tolerant as the second line of defense.
func hashRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(readRepositoryFile(t, path)))
	return hex.EncodeToString(sum[:])
}

func TestCanonicalCallersMatchTheBindingManifest(t *testing.T) {
	manifest := readBindingManifest(t)
	want := map[string]string{
		".github/workflows/ci.yml":                "hosting-platforms/github/workflows/callers/go/ci.yml",
		".github/workflows/codeql.yml":            "hosting-platforms/github/workflows/callers/go/codeql.yml",
		".github/workflows/dependency-review.yml": "hosting-platforms/github/workflows/callers/go/dependency-review.yml",
	}
	if len(manifest.Callers) != len(want) {
		t.Fatalf("the manifest carries %d callers, want %d", len(manifest.Callers), len(want))
	}
	for _, caller := range manifest.Callers {
		master, found := want[caller.File]
		if !found {
			t.Fatalf("the manifest carries an unexpected caller %q", caller.File)
		}
		if caller.Master != master {
			t.Fatalf("caller %q binds master %q, want %q", caller.File, caller.Master, master)
		}
		if hash := hashRepositoryFile(t, caller.File); hash != caller.SHA256 {
			t.Fatalf("the tenant caller %s hashes to %s, want the bound %s", caller.File, hash, caller.SHA256)
		}
		content := readRepositoryFile(t, caller.File)
		if !strings.Contains(content, "uses: "+manifest.Home.Repository+"/.github/workflows/reusable-") {
			t.Fatalf("the tenant caller %s does not reference a home payload", caller.File)
		}
		if !strings.Contains(content, "@"+manifest.Home.SHA) {
			t.Fatalf("the tenant caller %s does not pin the bound home SHA", caller.File)
		}
		if !strings.Contains(content, `branches: [main, develop, "release/**", "support/**"]`) {
			t.Fatalf("the tenant caller %s does not cover every shared line", caller.File)
		}
	}
}

func TestCanonicalFileFamilyMatchesTheBindingManifest(t *testing.T) {
	manifest := readBindingManifest(t)
	for _, topic := range []fileBinding{
		manifest.Files.Lefthook,
		manifest.Files.Gitattributes,
		manifest.Files.Dependabot,
	} {
		if hash := hashRepositoryFile(t, topic.Path); hash != topic.SHA256 {
			t.Fatalf("the canonical file %s hashes to %s, want the bound %s", topic.Path, hash, topic.SHA256)
		}
	}
	// The gitignore topic is prefix-mode in the home verifier: the canonical
	// core is a verbatim prefix and project additions live below the mark.
	gitignore := readRepositoryFile(t, manifest.Files.Gitignore.Path)
	const canonicalGitignoreCore = "# Local build and test outputs.\n/.build/\n/dist/\n/coverage/\n/.cache/\n*.coverprofile\n*.test\n*.out\n*.cov\n\n# -- project additions below this line --\n"
	if !strings.HasPrefix(gitignore, canonicalGitignoreCore) {
		t.Fatal("the gitignore does not carry the canonical core as a verbatim prefix")
	}
	for _, addition := range []string{".env", "*.pem", "*.key", "*.crt"} {
		if !strings.Contains(gitignore, addition) {
			t.Fatalf("the gitignore does not preserve the credential file pattern %q below the mark", addition)
		}
	}

	codeowners := readRepositoryFile(t, manifest.Codeowners.Path)
	if !strings.Contains(codeowners, "* "+manifest.Codeowners.DefaultOwner) {
		t.Fatalf("the ownership file does not bind the default owner %q", manifest.Codeowners.DefaultOwner)
	}
}

func TestConformanceWorkflowBindsTheVerifier(t *testing.T) {
	manifest := readBindingManifest(t)
	content := readRepositoryFile(t, ".github/workflows/canonical-conformance.yml")
	for _, required := range []string{
		"permissions: {}",
		"name: Canonical conformance",
		"uses: " + manifest.Home.Repository + "/.github/actions/verify-canonical-files@" + manifest.Home.SHA,
		`branches: [main, develop, "release/**", "support/**"]`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("the canonical conformance workflow does not contain %q", required)
		}
	}
}

func TestWorkflowsCarryNoTenantOrSecretValues(t *testing.T) {
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/codeql.yml",
		".github/workflows/dependency-review.yml",
		".github/workflows/canonical-conformance.yml",
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
		Toolchain     struct {
			Language string `json:"language"`
			Version  string `json:"version"`
		} `json:"toolchain"`
		Extends []string `json:"extends"`
		Project struct {
			Binaries []struct {
				Package string `json:"package"`
			} `json:"binaries"`
		} `json:"project"`
		Gates []struct {
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"gates"`
	}
	if err := json.Unmarshal([]byte(readRepositoryFile(t, "git-governance.quality.json")), &quality); err != nil {
		t.Fatalf("decode quality configuration: %v", err)
	}
	if quality.SchemaVersion != 4 {
		t.Fatalf("schemaVersion = %d, want 4", quality.SchemaVersion)
	}
	if quality.Toolchain.Language != "go" || quality.Toolchain.Version != "1.26.6" {
		t.Fatalf("toolchain = %q@%q, want the language-keyed go@1.26.6 form", quality.Toolchain.Language, quality.Toolchain.Version)
	}
	if quality.Extends == nil || len(quality.Extends) != 0 {
		t.Fatalf("extends = %v, want the explicit empty list", quality.Extends)
	}

	want := map[string]string{
		"platform-source-quality": "go tool -modfile tools/go.mod quality-gate",
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

	if len(quality.Project.Binaries) != 1 || quality.Project.Binaries[0].Package != "./cmd/broker" {
		t.Fatal("the project binaries must carry only the broker")
	}
	raw := readRepositoryFile(t, "git-governance.quality.json")
	for _, forbidden := range []string{`"./cmd/build"`, `"./cmd/check-coverage"`, `"defaults"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("git-governance.quality.json still contains %s", forbidden)
		}
	}
	for _, chainCopy := range []string{"cmd/build", "cmd/check-coverage"} {
		if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(chainCopy))); !os.IsNotExist(err) {
			t.Fatalf("the repo-local gate chain copy %s must not exist", chainCopy)
		}
	}
}

func TestLocalFortressContracts(t *testing.T) {
	assertWorkflowContract(t, "lefthook.yml", []string{
		"commit-msg:",
		`git-governance --interactive never commit validate --message-file "{1}"`,
		"pre-push:",
		`git-governance --interactive never validate pre-push --remote "{1}"`,
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
		"go tool -modfile tools/go.mod quality-gate",
		"approved internal Go proxy",
		"evidence-verified internal Go 1.26.6 builder artifact",
		"Tenant App keys",
	})
}

func TestOrganizationRulesetAdoptionHasNoLocalLegacyDefinitions(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "docs", "hosting-platforms")); !os.IsNotExist(err) {
		t.Fatalf("legacy ruleset location must not exist")
	}

	conventions := readRepositoryFile(t, "docs/conventions/hosting-plattform/github/rule-sets/README.md")
	for _, required := range []string{
		"git-governance",
		"quality-gates=linux-only",
		"~ALL",
	} {
		if !strings.Contains(conventions, required) {
			t.Fatalf("rule-set conventions README does not contain %q", required)
		}
	}
}

func TestModuleIdentityMatchesOrganizationNamespace(t *testing.T) {
	goMod := readRepositoryFile(t, "go.mod")
	for _, required := range []string{
		"module github.com/t33n-software/credential-broker-platform",
		"go 1.26",
		"toolchain go1.26.6",
	} {
		if !strings.Contains(goMod, required) {
			t.Fatalf("go.mod does not contain %q", required)
		}
	}
}

func TestGoToolchainAndBuildToolingContract(t *testing.T) {
	toolsMod := readRepositoryFile(t, filepath.Join("tools", "go.mod"))
	for _, required := range []string{
		"module github.com/t33n-software/credential-broker-platform/tools",
		"toolchain go1.26.6",
		"github.com/evilmartians/lefthook/v2",
		"golang.org/x/vuln/cmd/govulncheck",
		"honnef.co/go/tools/cmd/staticcheck",
		"github.com/t33n-software/go-quality-authority/cmd/quality-gate",
		"github.com/t33n-software/go-quality-authority/cmd/check-coverage",
		"github.com/t33n-software/repository-governance/cmd/verify-canonical",
	} {
		if !strings.Contains(toolsMod, required) {
			t.Fatalf("tools/go.mod does not contain %q", required)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "tools", "go.sum")); err != nil {
		t.Fatalf("tools/go.sum is missing: %v", err)
	}

	manifest := readBindingManifest(t)
	for _, caller := range []string{"ci.yml", "codeql.yml"} {
		content := readRepositoryFile(t, ".github/workflows/"+caller)
		if !strings.Contains(content, "uses: "+manifest.Home.Repository+"/.github/workflows/reusable-") {
			t.Fatalf("the caller %s does not reference a home payload", caller)
		}
	}

	traceability := readRepositoryFile(t, filepath.Join("docs", "TRACEABILITY.md"))
	if !strings.Contains(traceability, "CBP-3") {
		t.Fatal("TRACEABILITY.md does not contain CBP-3")
	}
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
