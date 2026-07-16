---
id: WI-012
title: Implement guarded SMB and NFS file services
status: done
priority: P1
owner: "/root"
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/controlpanel
  - internal/synology/operations/fileservices
  - internal/synology/controlpanel.go
  - internal/synology/compatibility_report.go
  - internal/application
  - internal/runtime
  - internal/cli
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-012 — Implement guarded SMB and NFS file services

## Outcome

A CLI user or MCP agent can inspect global SMB and NFS service configuration,
discover independently supported operations, and plan/apply supported changes
without exposing a generic DSM settings proxy.

## Scope

- Normalized global SMB service state: enabled state, workgroup,
  minimum/maximum protocol, transport encryption, and signing
  policy when explicitly available from the selected DSM backend.
- Normalized global NFS service state: enabled state, advertised protocol
  versions, NFSv4/v4.1 state, and read-only NFSv4 domain when explicitly
  available.
- Independent read and set compatibility selections for SMB and NFS.
- Shared hash-bound plan/apply contract with patch-only ownership, stale-state
  rejection, service-disruption warnings, and postcondition verification.
- Thin CLI and MCP adapters over the same application methods.

## Non-goals

- Per-shared-folder NFS host export rules; track these as a share-level follow-up.
- NFS advanced-setting writes until the full port, packet-size, UNIX permission,
  and service-state preservation snapshot has a stable typed contract.
- SMB ACLs, directory permissions, Active Directory/domain join, LDAP, Kerberos,
  WS-Discovery, Bonjour, rsync, FTP, SFTP, AFP, or WebDAV.
- Restarting unrelated services or exposing raw DSM request fields.
- Live service mutations without explicit authorization for the exact test.

## Design constraints

- SMB and NFS remain separate operation boundaries even if DSM returns them in
  one Control Panel page.
- Update intent is patch-only; omitted fields preserve their current value.
- Disabling a service or changing an active protocol/security policy is high
  risk and requires an approval hash based on the complete observed module state.
- API/version evidence must come from DSM API discovery and NAS-local Admin
  Center assets or official Synology documentation.

## Acceptance criteria

- [x] SMB and NFS state decoders expose only stable semantic fields.
- [x] Read and base-setting support is selected independently per protocol.
- [x] CLI and MCP schemas use the same application contract.
- [x] Apply rejects stale service state and verifies a fresh postcondition.
- [x] Request-capture tests lock every enabled DSM mutation shape.
- [x] DSM 7.3.2 read-only state/capability verification passes.
- [x] No live SMB/NFS mutation ran without new explicit authorization.

## Verification

- Sanitized decoder fixtures and request-capture unit tests.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only API discovery/state checks on the configured DSM 7.3.2 NAS.

## Coordination

This item extends the focused Control Panel module pattern established by
WI-006 and touches the same registry/facade/application adapter files. The user
explicitly requested starting SMB/NFS File Services work on 2026-07-16. No
live file-service mutation is authorized.

## Completion record

- Completed end to end on 2026-07-16 with typed SMB/NFS state, independently
  selected read/set variants, hash-bound plan/apply, CLI commands, MCP tools,
  request-capture tests, and user documentation.
- Read-only DSM 7.3.2 verification selected SMB v3, NFS v3, and NFS advanced
  read v1. SMB returned an enabled `WORKGROUP` configuration with SMB2 through
  SMB3; NFS returned enabled NFS2/NFS3 support plus advertised NFSv4/NFSv4.1.
- Live SMB and NFS plans were generated successfully, but no plan was applied
  and no File Services `set` method was called.
- NFS advanced writes deliberately remain unsupported because the DSM form
  submits a broader preservation snapshot than the current stable domain owns.
- Verified with `go test ./... -count=1`, `go vet ./...`, both command builds,
  and `git diff --check`.
