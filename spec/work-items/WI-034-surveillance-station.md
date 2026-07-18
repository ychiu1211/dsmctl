---
id: WI-034
title: Synology Surveillance Station read module
status: done
priority: P2
owner: ""
depends_on: [WI-019, WI-022, WI-029]
parallel_group: C
touches:
  - internal/domain/surveillance
  - internal/synology/operations/surveillance
  - internal/synology/surveillance.go
  - internal/application/surveillance.go
  - internal/cli/surveillance.go
  - internal/mcpserver/server.go
---

# WI-034 — Synology Surveillance Station read module

## Outcome

A read-only Surveillance Station module mirroring the Drive Admin (WI-022) and
Photos (WI-030) modules: system information and the configured camera inventory,
package-version gated on the installed SurveillanceStation package.

## Scope

- `SYNO.SurveillanceStation.Info` `GetInfo`: version, hostname, camera count, max
  camera support, license count, timezone.
- `SYNO.SurveillanceStation.Camera` `List`: configured cameras (id, name, IP,
  vendor, model, enabled).
- Package-version gating on `SurveillanceStation`; both APIs are read at the
  discovered max version (older `GetInfo` versions omit hostname/timezone).
- CLI (`surveillance capabilities|info|cameras`, alias `svs`) and three MCP read
  tools.

## Non-goals

- Recording, event/action-rule, live-view, license, and notification management.
- Camera add/edit/delete and any guarded write (a later work item).

## Design constraints

- Field names confirmed live on Surveillance Station 9.2.5 (DSM 7.3.2). Notable:
  `GetInfo` returns the version parts as **strings**
  (`{"major":"9","minor":"2","small":"5","build":"11979"}`) and misspells the
  license field `liscenseNumber`; the decoder tolerates both string/number forms
  and renders `major.minor.small-build`. The camera name is `newName`.
- Reads are issued at the API's discovered max version (Version 0) because v1
  `GetInfo` omits hostname/timezone.

## Acceptance criteria

- [x] Info + camera decode with semantic fields; decoder tests lock the live
      version-object shape and the camera list.
- [x] Package-version gating: reads fail closed without SurveillanceStation.
- [x] CLI + three MCP read tools (`get_surveillance_capabilities`,
      `get_surveillance_info`, `get_surveillance_cameras`).
- [x] DSM 7.3.2 live verification: read info (v9.2.5, 75 max cameras, 2 licenses,
      hostname/timezone) and the (empty) camera list on the lab.

## Installation note

Surveillance Station was installed on the lab **via the CLI** (`dsmctl package
install SurveillanceStation`) after WI-029 was extended to resolve dependencies:
it requires `SurveillanceVideoExtension`, which the installer now installs first.

## Verification

- Decoder tests; `go test ./... -count=1`, `go vet ./...`, CLI and MCP builds.
- Live read on the DSM 7.3.2 lab NAS (Surveillance Station 9.2.5).
