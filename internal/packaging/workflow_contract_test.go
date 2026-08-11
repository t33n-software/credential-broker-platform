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
			"persist-credentials: false",
			"go run -mod=readonly ./cmd/build",
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
			"CyberT33N",
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
