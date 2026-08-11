# Credential Broker Platform

`credential-broker-platform` is the tenant-neutral source foundation for
immutable credential-broker platform artifacts.

It mints short-lived, repository-bound GitHub App installation credentials from
a server-side deployment configuration. It does not create branches, commits,
pull requests, tags, releases, or shared-line mutations.

## Trust boundary

The platform source contains only reusable primitives:

- strict request and repository-allowlist validation;
- server-side fixed credential profiles;
- GitHub App JWT and installation-token minting;
- bounded HTTP handling, redacted failures, and graceful shutdown.

It intentionally excludes every tenant value:

- App IDs, installation IDs, private keys, repository allowlists, and service
  URLs;
- workload identities, IAM bindings, runtimes, registries, and evidence
  writers;
- tenant deployment or application-release evidence.

Each tenant consumes a reviewed immutable platform artifact and deploys its own
private broker service with its own configuration, credentials, and evidence.

## HTTP contract

```text
GET  /healthz
POST /v1/github/installations/token
```

The token request identifies a repository:

```json
{
  "host": "github.com",
  "owner": "example-owner",
  "repository": "example-repository"
}
```

The request cannot choose or expand permissions. The deployment selects one
fixed server-side credential profile through `BROKER_CREDENTIAL_PROFILE`.

## Fixed credential profiles

| Profile | Installation-token permissions |
| --- | --- |
| `release-automation` | `actions: write`, `contents: read`, `pull_requests: write` |
| `reconciliation-publisher` | `contents: write`, `pull_requests: write` |
| `hotfix-propagation-publisher` | `contents: write`, `pull_requests: write` |
| `release-credential-verification` | `contents: read` |
| `hotfix-delivery` | `actions: read`, `contents: read`, `pull_requests: read` |

Each tenant deployment chooses exactly one profile. Empty, untrusted, or
client-provided profiles are rejected.

## Runtime configuration

All values are deployment configuration; secret material remains outside the
repository.

```text
PORT                         optional; default 8080
BROKER_ALLOWED_REPOSITORIES  required; comma-separated host/owner/repository values
BROKER_APP_ID                required; GitHub App ID
BROKER_APP_INSTALLATION_ID   required; approved installation ID
BROKER_CREDENTIAL_PROFILE    required; one fixed profile above
BROKER_PRIVATE_KEY_PATH      required; mounted private-key file path
BROKER_API_BASE_URL          optional; HTTPS GitHub API base URL
BROKER_REQUEST_TIMEOUT       optional; default 10s
BROKER_MAX_REQUEST_BYTES     optional; default 4096
BROKER_MIN_TOKEN_LIFETIME    optional; default 2m
```

## Local verification

```powershell
go run -mod=readonly ./cmd/build
go test -mod=readonly ./...
go run -mod=readonly ./cmd/check-coverage
```

The build command enforces formatting, module integrity, read-only module
metadata, tests, exact statement coverage, race detection, static analysis,
Linux/AMD64 compilation, and embedded module provenance.

## Container input

`Dockerfile` accepts only an explicit `BUILDER_IMAGE` argument. The value must
be a full immutable internal builder-artifact reference. A public `golang`
image, mutable tag, or cache is not a valid substitute.

```powershell
$env:BUILDER_IMAGE = "<verified-internal-builder@sha256:...>"
docker build --build-arg "BUILDER_IMAGE=$env:BUILDER_IMAGE" --tag credential-broker-platform:dev .
```

`CBP-1` establishes the platform source boundary and portable source gates.
Tenant configuration, platform artifact delivery, builder authorities, and
cloud deployment remain separate governed work.
