# Traceability

## Tickets

| Ticket | Change | Status |
|---|---|---|
| CBP-1 | Establish the credential broker platform source boundary: tenant-neutral HTTP token endpoint with strict request and repository-allowlist validation, fixed server-side credential profiles, runtime configuration, container input contract, source-quality gates, CodeQL, dependency admission review, Dependabot, Lefthook, and importable Rulesets. | In progress |
| CBP-2 | Migrate the module path and Go import paths to the `t33n-software` organization namespace; add the LF line-ending contract (`.gitattributes`) and the push-protections Ruleset source `00-push-protections.json` in the verified GitHub export format. | In progress |
| CBP-3 | Align the Go 1.26.6 toolchain and source gates with the supply chain fortress contract: pinned `tools/` module with govulncheck, staticcheck, and Lefthook; fail-closed vulnerability analysis; existing configuration and HTTP boundary fuzz lanes integrated into the full source gate; Lefthook configuration validation and commit-msg hook; daily CI re-scan. | In progress |
| CBP-5 | Adopt the canonical repo surface: schema-v3 quality configuration, version surfaces on the tracked binaries, canonical tool pins (go-quality-authority v1.0.1, repository-governance verifier), the canonical file family and CODEOWNERS, the three byte-identical workflow callers, the tenant binding manifest, and the canonical conformance check. | In progress |
| CBP-7 | Reference the canonical gate chain through the tooling module pin: the repo-local chain copies `cmd/build` and `cmd/check-coverage` are removed (`cmd/broker` stays as the platform's own tool), the quality configuration invokes the go-quality-authority orchestrator via `go tool -modfile tools/go.mod quality-gate`, the restated `defaults` block is dropped for the schema-owned `includeFamilies` default (SCG-9), and the contract guards prove the canonical invocation, the absent `defaults` block, and the absence of both copies fail-closed. | In progress |

## Scope boundaries

- CBP-1 delivers the tenant-neutral platform source only. It does not deliver
  tenant configuration, platform artifact delivery, builder authority
  integration, cloud deployment, or a versioned release.
- The platform artifact delivery and release lifecycle follow only after a
  verified Go builder artifact exists.
- The `release/*` and `support/*` branch families and their Rulesets are
  activated only with a complete governed release lifecycle.
