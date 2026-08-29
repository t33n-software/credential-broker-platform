# Credential Broker Platform verification contract

## Local safe verification

Developers may run the platform source gate without tenant credentials:

```text
go tool -modfile tools/go.mod quality-gate
go test ./...
go tool -modfile tools/go.mod check-coverage
```

The canonical gate chain verifies:

```text
Go formatting
go mod verify
go mod tidy -diff
tools module download, verify, and tidy -diff
staticcheck lint
go test -mod=readonly ./...
100% statement coverage
go test -race
go vet
govulncheck fail-closed vulnerability analysis
configuration and HTTP boundary fuzz lanes
Lefthook configuration validation
native broker build and smoke test
```

The Go toolchain is pinned exactly (`toolchain go1.26.6`,
`GOTOOLCHAIN=local`); no lane downloads a toolchain at build time. Build tools
(`govulncheck`, `staticcheck`, `lefthook`) live in the separate pinned
`tools/` module with its own verified `go.mod` and committed `go.sum`; they
never join the source module graph. CI re-runs the full gate on a daily
schedule so newly disclosed vulnerabilities in the pinned toolchain or
dependency graph fail closed even without source changes. Lefthook provides
the local `commit-msg` hook (governed commit-message validation) and the
pre-push validation through `git-governance --interactive never validate
pre-push`.

Local verification must not use production GitHub App private keys, Cloud Run
invoker identities, deployment credentials, or a mutable production service.

## CI source-quality verification

The shared-line workflows are the byte-identical canonical callers of the
repository-governance home, pinned by full-length commit SHA: `ci.yml` runs
the canonical quality gate of the go-quality-authority territory home (check
context `Quality gates / linux-amd64`), `codeql.yml` runs the canonical
CodeQL lane (check context `CodeQL / CodeQL (go)`, consumed by the
code-scanning rule-set rule), and `dependency-review.yml` runs the dependency
admission review (check context `Dependency review / Dependency admission
review`). The callers trigger on push and pull request to every shared line
(`main`, `develop`, `release/**`, `support/**`) plus a daily schedule and
manual dispatch. The `canonical-conformance.yml` workflow runs the home's
conformance verifier (check context `Canonical conformance`) against
`repo-bindings.json`: caller hashes and pins, canonical file equality,
CODEOWNERS materialization, config-seam conformance, tool-pin admission, and
license-lane wiring. The organization rule-sets bind a check context only
after the lane has proven it on a real pull request to the exact target line.

## External Fortress prerequisites

Platform artifact delivery must not substitute this source-quality gate. The
following controls remain intentionally fail-closed until a separate,
tenant-neutral platform-delivery authority provisions them:

```text
approved internal Go proxy
separately issued and evidence-verified internal Go 1.26.6 builder artifact
platform artifact and evidence registries for SBOM, provenance, signatures,
attestations, promotion, and deployment evidence
```

No build, CI, or release workflow may substitute public-network fallback,
automatic toolchain download, or unapproved runtime tooling for those controls.
Tenant App keys, tenant runtime identities, tenant GCP resources, and
tenant-specific deployment evidence remain outside this repository.
