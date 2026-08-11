# Credential Broker Platform verification contract

## Local safe verification

Developers may run the platform source gate without tenant credentials:

```text
go run -mod=readonly ./cmd/build
go test ./...
go run ./cmd/check-coverage
```

The controlled build command verifies:

```text
Go formatting
go mod verify
go mod tidy -diff
go test -mod=readonly ./...
100% statement coverage
go test -race
go vet
Linux AMD64 broker build
embedded module provenance
```

Local verification must not use production GitHub App private keys, Cloud Run
invoker identities, deployment credentials, or a mutable production service.

## GitHub Code Quality and Coverage

The CI workflow generates an aggregate atomic Go coverage profile, converts it
to Cobertura XML with `cmd/coverage-cobertura`, and uploads it through the
pinned GitHub Code Coverage action.

Before activating the `code_quality` and `code_coverage` Ruleset rules, verify
in GitHub that:

```text
- Code Quality is enabled for this repository;
- the default branch has completed a Code Quality scan;
- a same-repository pull request reports Code Quality results;
- Cobertura coverage is uploaded for the default branch and pull request;
- github-code-quality[bot] reports the coverage result;
- the 100% minimum and zero-drop Ruleset evaluation succeeds.
```

The local 100% statement coverage gate and GitHub-native coverage are
complementary; neither replaces the other.

## CI source-quality verification

The required CI check is:

```text
Quality gates (linux-amd64)
```

It additionally runs deterministic fuzz smoke tests for configuration and HTTP
request boundaries, then verifies the Linux AMD64 platform binary.

## External Fortress prerequisites

Platform artifact delivery must not substitute this source-quality gate. The
following controls remain intentionally fail-closed until a separate,
tenant-neutral platform-delivery authority provisions them:

```text
approved internal Go proxy
separately issued and evidence-verified internal Go 1.26.5 builder artifact
platform artifact and evidence registries for SBOM, provenance, signatures,
attestations, promotion, and deployment evidence
```

No build, CI, or release workflow may substitute public-network fallback,
automatic toolchain download, or unapproved runtime tooling for those controls.
Tenant App keys, tenant runtime identities, tenant GCP resources, and
tenant-specific deployment evidence remain outside this repository.
