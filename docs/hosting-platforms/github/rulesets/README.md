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
GitHub Code Quality with severity all
GitHub Code Coverage with minimum coverage 100 and maximum drop 0
```

`Quality gates (linux-amd64)` enforces formatting, module integrity, tests,
exact 100% statement coverage, race detection, static analysis, Linux/AMD64
build, and module provenance.

## GitHub-native quality and coverage activation

The JSON files are the final platform target:

```text
Code Quality:
severity = all

Code Coverage:
minimum_coverage = 100
max_coverage_drop = 0
```

The CI workflow generates a combined atomic Go profile, converts it to
Cobertura XML, and uploads it through the immutable
`actions/upload-code-coverage` action with only `code-quality: write` added to
the normal read-only workflow permissions.

Before importing `02-develop.json` or `03-main.json`, prove:

```text
- GitHub Code Quality enabled and reporting a successful result;
- Cobertura XML uploaded for the default branch and pull requests;
- workflow permission code-quality: write;
- coverage reports present for the exact shared-line targets.
```

Do not weaken or remove these Ruleset rules to merge a PR. A missing
GitHub-native result blocks the merge by design and must be repaired at the
workflow or GitHub-feature boundary.

## Security boundary

Do not add tenant App IDs, installation IDs, tokens, private keys, WIF
providers, GCP variables, service URLs, or bypass actors to these files.
Ruleset imports are repository settings; committing a JSON file alone does not
modify GitHub.
