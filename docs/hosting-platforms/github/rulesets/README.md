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

Import in this order:

```text
01-ticket-working-branches.json
02-develop.json
03-main.json
```

No `release/*` or `support/*` Ruleset is imported until this platform has its
own governed release-line workflow and immutable artifact-delivery contract.

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
