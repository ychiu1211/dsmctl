---
id: WI-026
title: Guarded SMB advanced settings and service discovery
status: proposed
priority: P2
owner: ""
depends_on: [WI-012]
parallel_group: C
touches:
  - internal/domain/controlpanel
  - internal/synology/operations/fileservices
  - internal/synology/operations/servicediscovery
  - internal/synology/controlpanel.go
  - internal/application
  - internal/cli
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-026 — Guarded SMB advanced settings and service discovery

## Outcome

The SMB module exposes the DSM "Advanced Settings" surface beyond the WI-012
base six fields, and a separate service-discovery module exposes Bonjour,
WS-Discovery, and Time Machine over SMB. Both use the hash-bound plan/apply
flow.

## Scope

- SMB advanced settings (extending the `SYNO.Core.FileServ.SMB` snapshot):
  Local Master Browser, opportunistic locking, SMB durable handles, allow
  symbolic links within and across shared folders, veto files, WINS server,
  macOS-compatible extensions, and the transfer-log switch plus syslog target.
- A `SYNO.Core.FileServ.ServiceDiscovery` module: Bonjour service advertising,
  Time Machine broadcast over SMB, and WS-Discovery.
- Independent read/set selection and capability reporting for each surface.

## Non-goals

- Custom Windows ACL entries and share-level SMB permissions (WI-008).
- Active Directory / LDAP / domain join and Kerberos.
- SMB per-share overrides.

## Design constraints

- DSM evidence gathered 2026-07-18 (confirm exact field names/enums against
  source before wiring each variant; do not guess):
  - Service discovery is its own repo `webapi-ServiceDiscovery`. API
    `SYNO.Core.FileServ.ServiceDiscovery` v1 get/set has exactly
    `enable_smb_time_machine` and `enable_afp_time_machine`
    (`src/SYNO.Core.FileServ.ServiceDiscovery.cpp`). A sibling API
    `SYNO.Core.FileServ.ServiceDiscovery.WSTransfer` v1 get/set carries
    `enable_wstransfer` (WS-Discovery), registered in
    `synosamba/webapi/SYNO.Core.FileServ.SMB.cpp`. These are two independent
    compatibility boundaries; WS-Discovery may be absent while Time Machine
    advertising is present.
  - SMB advanced fields live in the same `SYNO.Core.FileServ.SMB` get/set v3
    that WI-012 already uses (`synosamba/webapi/SYNO.Core.FileServ.SMB.py`,
    `SYNO_Core_FileServ_SMB_3_set_spec`), plus SMB durable-handle/lease toggles
    sent via that API (`dsm-AdminCenter/modules/FileService/AdvancedTab.js`).
    Extending it touches the WI-012 SMB decoder/encoder and its request-capture
    test, so treat SMB-advanced as a distinct sub-slice from service discovery.
- SMB advanced set is full-snapshot: refresh, merge approved fields, submit,
  verify (reuse the WI-025 snapshot pattern). SMB and service discovery remain
  separate compatibility boundaries.
- Enabling symbolic-link following or disabling signing/oplock protections is
  high risk; a transfer-log or Time Machine/WS-Discovery toggle is medium.
- No live SMB or service-discovery mutation without new explicit authorization.

## Suggested slicing

1. Service discovery module (clean, isolated, mirrors WI-024): the two
   `ServiceDiscovery` Time Machine toggles plus the `WSTransfer` WS-Discovery
   toggle, with independent read/set selection per API.
2. SMB advanced fields as a follow-up sub-slice that extends the WI-012 SMB
   snapshot with oplock, durable handles, symbolic-link, local master browser,
   veto, WINS, macOS extension, and transfer-log fields once each field name is
   confirmed.

## Acceptance criteria

- [ ] SMB advanced fields decode with strict validation and semantic names.
- [ ] Service discovery is an independent module with its own capability row.
- [ ] Advanced/discovery apply preserves every unspecified snapshot field.
- [ ] Request-capture tests lock each enabled set shape.
- [ ] CLI and MCP reuse the file-service application contract.
- [ ] No live mutation ran without new explicit authorization.

## Verification

- Decoder fixtures and request-capture tests per surface.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only state/capability checks on the configured DSM 7.3.x NAS.

## Coordination

Shares the fileservices package with WI-012/WI-025 and `server.go` with the
other file-protocol items. Confirm the ServiceDiscovery API surface first.
