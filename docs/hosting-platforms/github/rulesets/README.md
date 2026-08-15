# GitHub Rulesets

These JSON files are the platform-specific GitHub Ruleset mapping for this
repository. They consume the canonical policy from
`GIT_GITHUB_RULESETS_BLUEPRINT_001`; they do not define a second branch,
release, evidence, or credential policy.

## Import order

Import through the GitHub graphical interface only:

```text
Settings
→ Rules
→ Rulesets
→ New ruleset
→ Import a ruleset
```

The branch Rulesets 01 to 03 bind required status checks; import them only
after the repository has produced the actual checks for the target line. The
push Ruleset 00 binds no required checks and no branch targets, so it does not
wait for check evidence.

Import in this order:

```text
00-push-protections.json
01-ticket-working-branches.json
02-develop.json
03-main.json
```

No `release/*` or `support/*` Ruleset is imported until this platform has its
own governed release-line workflow and immutable artifact-delivery contract.

## Push protections

`00-push-protections.json` is a push Ruleset: it applies to every push to the
repository and its entire fork network and carries no branch targeting. It
blocks secret- and key-shaped artifacts (private-key and key-store extensions,
environment files, credential files, and infrastructure state files) from
entering the commit graph.

Push Rulesets exist only for private and internal repositories with the Team
plan. This repository is public and cannot carry one; the file documents the
boundary and stands ready if the repository is ever reclassified as private.
The public secret-material boundary is secret scanning with push protection
plus the local quality gates.

The file mirrors the official GitHub export envelope because the import
validates against that schema: a `source` field with the repository's own
identity, an explicit `conditions: null` (push Rulesets carry no branch
conditions), and restricted file extensions in glob form (`*.pem`, not `pem`).

## Required GitHub repository settings

```text
Allow merge commits: enabled
Allow rebase merging: enabled
Allow squash merging: enabled
Automatically delete head branches: enabled
Allow auto-merge: disabled
Always suggest updating pull request branches: disabled
Enable release immutability: enabled
```

The working-branch Ruleset intentionally omits deletion protection so GitHub
can delete an eligible merged ticket branch. `main` and `develop` remain
deletion-protected.

## Shared-line checks

The JSON files require:

```text
Quality gates (linux-amd64)
Dependency admission review
CodeQL code scanning with all alerts blocking
```

`Quality gates (linux-amd64)` enforces formatting, module integrity, tests,
exact 100% statement coverage, race detection, static analysis, Linux/AMD64
build, and module provenance.

## Security boundary

Do not add tenant App IDs, installation IDs, tokens, private keys, WIF
providers, GCP variables, service URLs, or bypass actors to these files.
Ruleset imports are repository settings; committing a JSON file alone does not
modify GitHub.
