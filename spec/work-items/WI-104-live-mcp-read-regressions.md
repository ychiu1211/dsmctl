---
id: WI-104
title: Fix live MCP read regressions found by fleet audit
status: done
priority: P1
owner: codex
depends_on: [WI-041]
parallel_group: E
touches:
  - internal/synology/operations/hyperbackup/decode.go
  - internal/synology/operations/hyperbackup/operation_test.go
  - internal/synology/operations/externalaccess/operation.go
  - internal/synology/operations/externalaccess/operation_test.go
  - internal/synology/operations/securityadvisor/decode.go
  - internal/synology/operations/securityadvisor/operation_test.go
  - internal/application/download_station.go
  - internal/application/package_center.go
  - internal/mcpserver/server_test.go
  - internal/mcpserver/remote_policy.go
  - internal/mcpserver/remote_policy_test.go
  - internal/synology/packagecenter.go
  - internal/synology/packagecenter_test.go
---

# WI-104 — Fix live MCP read regressions found by fleet audit

## Outcome

The shipped Hyper Backup application inventory, External Access account
inventory, and Security Advisor status return normalized state across the three
configured DSM test targets instead of failing on valid target-specific wire
shapes. Download Station settings planning also returns an MCP-valid structured
plan instead of failing output-schema validation after successful planning.
Package Center's updates-only catalog also excludes repository builds older than
the version already installed on the NAS. Remote Snapshot Replication planning
and applying accept their native two-NAS shape and authorize both sites.

## Scope

- Accept the live Hyper Backup `depend.folder_list` shapes observed across the
  fleet: string entries, `folderPath` / `fullPath` descriptors, and the
  `folder` / `whitelist` descriptor returned by Hyper Backup 4.2.2. Preserve a
  stable path for each descriptor and fail closed when an object has no
  recognizable path.
- When `SYNO.Core.MyDSCenter.query` reports that no Synology Account is logged
  in, return that valid logged-out state without calling the optional
  `SYNO.Core.Package.MyDS.get` enrichment endpoint.
- Treat Security Advisor `sysStatus:"firstScan"` as a valid not-yet-scanned
  state with no completed severity, not as an unknown severity or active scan.
- Model the Download Station settings plan's observed group as a JSON object in
  both Go and the generated MCP output schema.
- Mark `update_available` only when the offered package version compares newer
  than the installed version, while preserving the update plan's independent
  downgrade guard; choose the same stable/beta offer the update planner uses.
- Teach remote policy about Snapshot Replication's `source_nas` / `dest_nas`
  plan shape, require both profiles in the caller allow-list, and record
  high-risk approval against the source profile revision.
- Add decoder/operation regression tests for all three live failures.
- Re-run the affected MCP reads against every configured NAS on which the matching
  capability is available.

## Non-goals

- Changing MCP schemas or removing fields from compatibility reports.
- Fixing the cross-cutting capability-report payload duplication, unsupported
  error categorization, or slow unsupported-operation rejection found by the
  same audit. Those findings require separate coordination with WI-060/WI-063.
- Hyper Backup task/application mutation behavior.
- Any live DSM mutation.

## Design constraints

- Preserve the shared CLI/MCP application boundary; fixes stay in the typed
  operation decoders/composition layer.
- Do not silently convert an unrecognized `folder_list` value into an empty
  successful state.
- Do not special-case DSM error 4571 when the core account state already proves
  that package enrichment is inapplicable.
- No secret or account token is decoded or returned.

## Acceptance criteria

- [x] Hyper Backup application decoding accepts string folder dependencies and
      live structured folder descriptors, returning stable required paths.
- [x] A structured folder descriptor without a recognizable path fails with a
      targeted decoder error.
- [x] A logged-out Synology Account read does not call package enrichment and
      returns `logged_in:false` without an error.
- [x] Security Advisor status accepts `firstScan` without reporting a completed
      severity or claiming that a scan is currently running.
- [x] Download Station settings planning returns an object-valued `observed`
      state that passes the advertised MCP output schema.
- [x] Package catalog update filtering excludes equal and older offered
      versions, including the live File Station downgrade case.
- [x] Remote Snapshot Replication plan/apply dispatch authorizes both named NAS
      profiles and records its source-bound high-risk approval request.
- [x] Existing logged-in account enrichment behavior and token redaction remain
      unchanged.
- [x] Focused Go tests pass and all affected MCP reads/plans are rechecked live.

## Verification

- `go test ./internal/synology/operations/hyperbackup ./internal/synology/operations/externalaccess -count=1`
- `go test ./internal/synology/... ./internal/application/... ./internal/mcpserver/... -count=1`
- `go test ./... -count=1`
- `git diff --check`
- Built and live-upgraded the final image/SPK as `7.3.2-28`; offline SPK
  validation passed and the remote upload SHA-256 matched before installation.
- Live regression: Hyper Backup returned 13 applications and normalized the
  `folder` descriptors to `surveillance` and `homes`.
- Live regression: logged-out External Access reads succeeded on
  `lab-default` and `lab-secondary`.
- Live regression: Security Advisor accepted the first-scan state on
  `lab-default` and `lab-secondary`.
- Live regression: Download Station planning returned object-valued
  `observed:{"update_interval_minutes":1440}` and passed MCP output validation.
- Live regression: Package Center `updates_only:true` returned no false update
  for the older offered File Station build.
- Live regression: two-NAS Snapshot Replication planning passed remote-policy
  routing and reached the expected package-capability gate on the source NAS.
- Live read-only MCP checks only; no mutation is authorized or required by this
  item.

## Coordination

- WI-087 remains in progress for the broader Hyper Backup surface. This item is
  an isolated corrective patch to an already-exposed read and does not change
  its unfinished write scope.
- WI-060 owns the structured error taxonomy and WI-063 owns schema-stability
  policy; this item records but does not edit those cross-cutting surfaces.
- WI-041 is done and supplies the External Access account composition contract.

## Handoff

- Local implementation and the full Go test suite pass. `git diff --check`
  reports only the repository's existing Windows line-ending notices.
- Gateway host `lab-gateway` is running `dsmctl-gateway 7.3.2-28`.
- Upgrades from `7.3.2-25` through the diagnostic `7.3.2-27` preserved state,
  credentials, and private keys. DSM created one complete recovery directory
  before each upgrade.
- The three uploaded `/tmp/dsmctl-gateway-7.3.2-{26,27,28}-x86_64.spk`
  artifacts were verified as exact `/tmp` paths, deleted after validation, and
  confirmed absent. No temporary NAS resource remains.
