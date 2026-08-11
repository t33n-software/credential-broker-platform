# Contributing

## Scope boundary

This repository contains generic credential-broker platform code only. Do not
add tenant App IDs, installations, private keys, repository allowlists,
workload identities, cloud resources, service URLs, or tenant evidence.

Tenant configuration and deployment consume reviewed immutable platform
artifacts from separate tenant-owned repositories.

## Branching

`develop` is the integration line and `main` is the published platform truth.
Use a ticket-bound working branch and a pull request to merge into a shared
line. Direct pushes, force pushes, rebases of shared lines, and static
credentials are prohibited.

`CBP-1` establishes the initial platform source foundation. Subsequent platform
work uses the `CBP` ticket namespace unless a later governance decision binds a
different platform key.

## Verification

Run after relevant Go changes:

```powershell
go run -mod=readonly ./cmd/build
```

The controlled build verifies formatting, module integrity, read-only module
metadata, tests, exact `100.0%` statement coverage, race detection, static
analysis, Linux/AMD64 compilation, and embedded module provenance.
