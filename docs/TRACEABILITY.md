# Traceability

## Tickets

| Ticket | Change | Status |
|---|---|---|
| CBP-1 | Establish the credential broker platform source boundary: tenant-neutral HTTP token endpoint with strict request and repository-allowlist validation, fixed server-side credential profiles, runtime configuration, container input contract, source-quality gates, CodeQL, dependency admission review, Dependabot, Lefthook, and importable Rulesets. | In progress |
| CBP-2 | Migrate the module path and Go import paths to the `t33n-software` organization namespace; add the LF line-ending contract (`.gitattributes`) and the push-protections Ruleset source `00-push-protections.json` in the verified GitHub export format. | In progress |
| CBP-3 | Align the Go 1.26.6 toolchain and source gates with the supply chain fortress contract: pinned `tools/` module with govulncheck, staticcheck, and Lefthook; fail-closed vulnerability analysis; existing configuration and HTTP boundary fuzz lanes integrated into the full source gate; Lefthook configuration validation and commit-msg hook; daily CI re-scan. | In progress |

## Scope boundaries

- CBP-1 delivers the tenant-neutral platform source only. It does not deliver
  tenant configuration, platform artifact delivery, builder authority
  integration, cloud deployment, or a versioned release.
- The platform artifact delivery and release lifecycle follow only after a
  verified Go builder artifact exists.
- The `release/*` and `support/*` branch families and their Rulesets are
  activated only with a complete governed release lifecycle.
