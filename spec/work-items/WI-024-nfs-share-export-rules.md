---
id: WI-024
title: Guarded per-shared-folder NFS export rules
status: done
priority: P1
owner: ""
depends_on: [WI-012]
parallel_group: C
touches:
  - internal/domain/nfsexport
  - internal/synology/operations/nfsexport
  - internal/synology/nfsexport.go
  - internal/synology/compatibility_report.go
  - internal/application
  - internal/runtime
  - internal/cli
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-024 — Guarded per-shared-folder NFS export rules

## Outcome

A CLI user or MCP agent can read the NFS export rule set of one shared folder,
discover whether the export backend is supported, and plan/apply a complete
desired rule set through the same hash-bound approval flow used by the other
file-service modules. This closes the first WI-012 non-goal ("per-shared-folder
NFS host export rules").

## Scope

- Read one shared folder's NFS export rules via
  `SYNO.Core.FileServ.NFS.SharePrivilege` v1 `load` (keyed by `share_name`).
- Normalized rule fields: client pattern, privilege (read-write / read-only),
  root squash mapping, security flavor, asynchronous writes, allow connections
  from non-privileged ports, and allow access to mounted subfolders.
- Independent read and set compatibility selection for the export backend.
- Full-desired-state ownership: a plan carries the complete replacement rule
  set for one shared folder; `save` submits the whole `rule` array. This mirrors
  the time module's `ntp_servers` set-replace field, not a per-rule patch.
- Hash-bound plan/apply with observed-state fingerprint, stale-state rejection,
  network-exposure warnings, and postcondition verification.
- Thin CLI and MCP adapters over one application contract.

## Non-goals

- Kerberos keytab upload and NFS ID-map management
  (`SYNO.Core.FileServ.NFS.Kerberos`, `.IDMap`); track separately.
- The ActiveBackup-only rule fields `fsid` and `share_subdir`.
- Creating, deleting, or otherwise mutating the shared folder itself
  (owned by the shared-folder mutation module).
- Enabling the global NFS service (owned by WI-012).

## Design constraints

- DSM evidence: API method table in `webapi-NFS/src/SYNO.Core.FileServ.NFS.cpp`
  (`SYNO.Core.FileServ.NFS.SharePrivilege` v1 `load`/`save`); rule field names
  and enumerations in `webapi-NFS/src/share_privilege.cpp`:
  `client`, `privilege` (`rw`/`ro`), `root_squash`
  (`root`/`admin`/`guest`/`all_admin`/`all_guest`), `async`, `insecure`,
  `crossmnt`, `security_flavor`
  (`sys`/`kerberos`/`kerberos_integrity`/`kerberos_privacy`), and `id`
  (blank to create, the previous `client` to rename).
- The domain exposes semantic enum names, never raw DSM strings. `insecure`
  becomes `allow_nonprivileged_ports`; `crossmnt` becomes
  `allow_subfolder_access`; `root_squash=root` becomes `no_mapping`.
- Because `save` replaces the whole set, apply reads the current rule set,
  rejects a changed observed fingerprint, submits the complete approved set,
  and re-reads to verify.
- Broadening access is high risk: a rule with a wildcard client (`*`) or a
  write privilege, or removing an existing restricting rule, is high risk;
  a strictly narrowing change is medium.
- No live NFS export mutation runs without new explicit authorization for the
  exact shared folder under test.

## Acceptance criteria

- [x] Export decoder exposes only stable semantic fields and rejects malformed
      responses instead of returning an empty rule set.
- [x] Read and set support is selected independently and reported in
      capabilities with API/version evidence.
- [x] CLI and MCP share the same application plan/apply contract.
- [x] Apply rejects stale observed state, submits the full desired rule set,
      and verifies a fresh postcondition.
- [x] Request-capture tests lock the `save` request shape, including `id`
      handling for create versus rename.
- [x] DSM 7.3.x `load` and guarded `save` verified on a real shared folder with
      explicit user authorization.
- [x] No live `save` ran without new explicit authorization (the 2026-07-18 run
      was explicitly authorized and fully reverted).

## Completion record

- Completed 2026-07-18 with typed NFS export rules over
  `SYNO.Core.FileServ.NFS.SharePrivilege` v1 load/save, hash-bound plan/apply
  (full desired-state ownership, observed fingerprint, stale rejection,
  exposure/removal warnings, postcondition verification), CLI under
  `control-panel file-services nfs export`, four MCP tools with read-only
  gating, docs, and unit/request-capture/plan-apply tests.
- Live DSM 7.3.2 verification (lab, explicitly authorized, fully reverted):
  read an empty rule set, added one read-only rule, added a second rule while
  the first was preserved (confirming `id` = old client for edits and `""` for
  creations and full-set replacement), then cleared all rules. NFS was enabled
  for the test and disabled afterward; the shared folder ended with no rules.
- Live finding and fix: DSM represents `security_flavor` as a boolean object
  (`{sys, kerberos, kerberos_integrity, kerberos_privacy}`) in both the load
  response and the save request, not a string. The decoder now selects the
  enabled flavor and the encoder emits the object with exactly one enabled;
  `save` returned code 2301 until this was corrected.
- Also confirmed live: `SharePrivilege.save` requires the NFS service to be
  running (returns 2301 while NFS is disabled).
- Verified with `go test ./... -count=1`, `go vet ./...`, and both builds.

## Verification

- Sanitized `load` decoder fixtures and `save` request-capture tests.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only export `load` on the configured DSM 7.3.x NAS.

## Coordination

Extends the file-service module family established by WI-012 and reuses the
shared-folder inventory to resolve share names. `internal/mcpserver/server.go`,
`internal/application/service.go`, and the compatibility report are
high-contention; coordinate with any concurrent WI-025/WI-026 owner. The user
approved starting this on 2026-07-18.
