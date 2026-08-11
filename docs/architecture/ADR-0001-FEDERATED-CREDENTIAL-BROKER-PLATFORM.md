# ADR-0001: Federated Credential Broker Platform

## Status

Accepted.

## Context

The former broker repository combines reusable credential-broker code with
Git-Governance tenant deployment, identity, registry, and evidence values.
That coupling prevents the code from becoming a central platform product
without also centralizing tenant credentials and runtime authority.

## Decision

This repository contains only the generic credential-broker platform core. It
produces reviewed, immutable platform artifacts that tenant configuration
projects may consume.

Every tenant must deploy a separate broker service instance with its own:

- SCM App and installation;
- private key and secret boundary;
- repository allowlist and fixed profile selection;
- workload identity, runtime, invoker, deployer, and promoter identities;
- artifact, evidence, audit, revocation, and incident boundaries.

The platform never receives tenant private keys, tenant-specific allowlists,
tenant runtime configuration, or cloud deployment authority.

## Consequences

- The platform centralizes source code and immutable platform artifacts, not
  tenant credential runtimes.
- A tenant configuration project pins a reviewed platform digest and owns its
  deployment and evidence.
- The broker exposes only a repository-bound installation-token endpoint and
  fixed permission profiles. It cannot mutate Git or release objects.
- Builder authorities, platform artifact delivery, tenant migration, and
  tenant cloud configuration are separate governed work.
