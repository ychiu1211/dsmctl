---
id: WI-025
title: Complete guarded NFS advanced-setting writes
status: done
priority: P1
owner: ""
depends_on: [WI-012]
parallel_group: C
touches:
  - internal/domain/controlpanel
  - internal/synology/operations/fileservices
  - internal/synology/controlpanel.go
  - internal/application/file_services.go
  - internal/cli/file_services.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-025 — Complete guarded NFS advanced-setting writes

## Outcome

The NFS module reports `set_advanced: true` and can write the NFSv4 ID-mapping
domain and the NFS packet-size and UNIX-permission advanced settings through the
existing hash-bound plan/apply flow. This removes the WI-012 fail-closed on
`nfsv4_domain` writes.

## Scope

- Model the full advanced snapshot DSM's
  `SYNO.Core.FileServ.NFS.AdvancedSetting` get/set exchanges (service state,
  custom-port switch, read/write packet size, UNIX-permission switch, statd and
  nlm ports, NFSv4 domain) so a write can preserve every unspecified field.
- Enable the `SelectNFSAdvancedSet`/`ExecuteNFSAdvancedSet` path with a complete
  merge-and-submit encoder: apply reads the whole snapshot, overrides only the
  approved field, and resubmits the full snapshot.
- Expose the NFSv4 domain as the first writable advanced field through the
  existing file-service plan/apply flow, still planned separately from NFS base
  settings (as WI-012 already requires for `nfsv4_domain`).

## Non-goals

- Per-shared-folder NFS export rules (WI-024).
- Kerberos and ID-map management APIs.
- Changing NFS base protocol enablement inside the advanced path.
- Exposing read/write packet size, custom NFS ports, and the UNIX-permission
  switch as mutations. They are modeled only to preserve them across a domain
  write; exposing them needs their DSM-permitted value sets confirmed first.

## Design constraints

- DSM evidence: `SYNO.Core.FileServ.NFS.AdvancedSetting` v1 `get`/`set` in
  `webapi-NFS/src/SYNO.Core.FileServ.NFS.cpp` and `src/nfsAdv.cpp`; the full
  advanced snapshot fields observed in `synoc2-ansible/cms/ds_configure.sh`:
  `nfs_v4_domain`, `read_size`, `write_size`, `unix_pri_enable`
  (with `enable_nfs`, `enable_nfs_v4`, `enabled_minor_ver` owned by base set).
- Advanced set is full-snapshot: apply refreshes the complete advanced state,
  merges only the approved fields, submits the whole snapshot, and verifies.
- `read_size`/`write_size` accept only the DSM-permitted discrete values;
  reject anything else before any write.
- Changing the NFSv4 domain or packet size can disrupt active clients and is
  high risk; toggling UNIX permissions is high risk.
- Domain writes still require NFSv4 to be enabled, matching DSM behavior.
- No live advanced `set` runs without new explicit authorization.

## Acceptance criteria

- [x] NFS advanced snapshot decodes service state, packet sizes, ports, the
      UNIX-permission switch, and domain with strict validation of the always-
      present fields.
- [x] `set_advanced` is reported `true` only when the advanced set backend is
      selected.
- [x] Advanced apply refreshes and preserves every unspecified snapshot field
      (request-capture test locks the full `set` snapshot with only the domain
      changed).
- [x] CLI and MCP expose the NFSv4 domain write through the existing
      file-service plan/apply tools.
- [x] DSM 7.3.x advanced `get` and guarded `set` verified on a real NAS with
      explicit user authorization.
- [x] No live advanced `set` ran without new explicit authorization (the
      2026-07-18 run was explicitly authorized and fully reverted).

## Completion record

- Completed 2026-07-18: the NFSv4 domain is writable through the file-service
  plan/apply flow over `SYNO.Core.FileServ.NFS.AdvancedSetting` v1; `set_advanced`
  reports true when the backend is selected; the write reads the whole advanced
  snapshot, overrides only the domain, and resubmits it.
- Live DSM 7.3.2 verification (lab, explicitly authorized, fully reverted): set
  the NFSv4 domain to a test value and restored it to empty, with the NFS
  service left disabled and the domain empty afterward.
- Live findings and fixes (the write returned code 2301 until corrected):
  1. The AdvancedSetting `get` omits `enable_nfs`, but the `set` requires it and
     uses it as the service run flag. The facade now supplies the current base
     NFS service state so the write never toggles the service.
  2. The `get` returns `custom_port_enable` as an integer, but the `set`
     validation requires a boolean. The snapshot is now decoded to typed values
     and re-encoded with the boolean types the set expects, rather than passed
     through raw.
  An earlier "NFSv4 must be enabled" plan guard was removed: it was based on a
  wrong hypothesis (the domain write succeeds with NFS disabled once the payload
  is correct).
- Verified with `go test ./... -count=1`, `go vet ./...`, and both builds.

## Verification

- Advanced `get` decoder fixture and advanced `set` request-capture test.
- `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Read-only advanced `get` on the configured DSM 7.3.x NAS.

## Coordination

Edits the same fileservices package and `file_services.go` application/CLI as
WI-012 and shares `internal/mcpserver/server.go` with WI-024/WI-026. Only one
owner should hold the fileservices package at a time.
