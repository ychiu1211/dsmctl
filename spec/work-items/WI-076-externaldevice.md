---
id: WI-076
title: External Devices module (USB/eSATA storage, printers)
status: proposed
priority: P3
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/externaldevice
  - internal/synology/operations/externaldevice
  - internal/synology/externaldevice.go
  - internal/runtime/manager.go
  - internal/application/externaldevice.go
  - internal/cli/externaldevice.go
  - internal/mcpserver/server.go
  - internal/mcpserver/read_only.go
  - docs/control-panel.md
---

# WI-076 — External Devices module (USB/eSATA storage, printers)

## Outcome

A CLI user or MCP agent can read the Control Panel → External Devices surface —
attached USB and eSATA disks (partitions, filesystem, size, usage, mount and
safe-to-remove status) and connected printers (USB/network printers, spooler
state, default and sharing settings) — and, through the hash-bound plan/apply
contract, safely **eject** a device and change **printer** settings under
guardrails. This is a focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting
area, never a generic `set key=value` proxy. Eject in particular is exposed as a
first-class typed action, not a raw method passthrough, precisely because its
blast radius (unflushed writes on a busy device) demands the plan/apply
guardrail.

The API families, methods, and field names below are the author's best current
knowledge and **must be live-verified at implementation time**: the standing
policy is that source-doc and mobile-client field names are often stale, so each
API and its response shape must be confirmed against the lab with a throwaway
`DSMCTL_DUMP` read-only probe before any decoder or request is trusted
(see [[dsm-webapi-live-verify-fields]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only

- **External storage (USB):** likely `SYNO.Core.ExternalDevice.Storage.USB`
  (`list`, or a versioned `get`) → per-device `{dev_id/dev_path, dev_title,
  product/vendor, dev_type, total_size_mb, status}` plus a `partitions` array
  `{name/dev_path, filesystem, total_size_mb, used_size_mb, mount_point,
  share_name, status}`. Whether the eject-readiness / busy flag ships in the
  same item or a separate probe is to be live-verified.
- **External storage (eSATA):** likely `SYNO.Core.ExternalDevice.Storage.eSATA`
  with the same item/partition shape, as an **independent** area (many models
  have no eSATA port).
- **Printers:** likely `SYNO.Core.ExternalDevice.Printer` (`list` / `get`) →
  per-printer `{id, name, type (usb/network), status, manager, default,
  spooler_count/spooler_status}`, plus global print-service settings
  (network-printing mode, Bonjour/AirPrint sharing) if the same family exposes
  them.
- **Capabilities:** report each area (USB storage, eSATA storage, printer) with
  a stable operation name, selected backend, API, and version; an area whose API
  is absent is reported `(not supported)` and never fails the module.

### Slice B — guarded write (plan/apply, hash-bound)

- **Eject a device** — the eject method on the relevant storage family
  (`SYNO.Core.ExternalDevice.Storage.USB` / `.eSATA`, method name and whether it
  keys on `dev_id` vs `dev_path` to be live-verified). Plan binds the observed
  device state (identity, partitions, mount points, size, and any busy/in-use
  flag) and hashes it; apply re-reads, **rejects stale state** (a different
  device now on that port, a partition newly mounted or busy), performs the typed
  eject, and re-reads to confirm the device is gone from the list or marked
  safe-to-remove.
- **Printer settings** — `SYNO.Core.ExternalDevice.Printer` `set` for the typed,
  reversible knobs (printer name, default printer, manager/print-mode, sharing
  toggles). Patch-only ownership: unspecified fields are never reset.
- **Printer spooler clear** — clearing queued print jobs for a printer (method
  on the printer family or a `...Printer.Spooler`-style sub-API, to be
  live-verified). Exposed as a distinct typed action because it discards queued
  jobs.

## Non-goals

- **UPS** (`SYNO.Core.ExternalDevice.UPS` / `SYNO.Core.Hardware.UPS`). UPS lives
  under a separate Control Panel area (Hardware & Power) with its own network-UPS
  and shutdown-timing semantics; it is a separate work item.
- **Formatting, partitioning, or repairing external disks**, and mounting a USB
  disk as a Storage Pool / volume member. Those are destructive storage
  operations belonging to the storage lineage (WI-001..003), not an
  external-devices settings patch.
- **Print job submission / management beyond spooler clear** (inspecting or
  reordering individual jobs), and printer **driver** upload/management.
- **Network-printer provisioning** (adding a remote/IPP printer by address). This
  WI manages printers DSM already enumerates.
- **Global print-service package management** (installing/removing a virtual
  print server package) — that is Package Center (WI-019/029).

## Design constraints

- **Independent compatibility boundaries.** USB storage, eSATA storage, and
  printers are separate API families and separate failure boundaries. A NAS with
  no eSATA port, or no printer support, must leave the other areas fully usable,
  each reported `(not supported)` rather than erroring the whole module.
  Selection is per operation (per the compatibility framework), and the module
  **fails closed** when a required API is absent.
- **Eject is HIGH risk — data safety.** Ejecting a device that has unflushed
  writes or an actively used share can lose data and drops a mounted volume from
  availability; classify every eject plan HIGH. The plan/apply staleness check
  is the primary safety mechanism: the observed busy/mount fingerprint at plan
  time must still hold at apply time, and apply aborts if the device changed.
- **Spooler clear is destructive.** Clearing the spooler discards queued jobs;
  classify it high (destructive, though small blast radius) and require the same
  plan/apply confirmation as eject. Printer name/default/sharing changes are
  reversible config; classify medium.
- **Patch + postcondition.** Follow the module pattern: plan records and hashes
  the complete current state, apply rejects a changed state, merges the patch
  into a freshly read state, and **re-reads to verify** the requested fields
  actually took effect — DSM silently ignores some fields (e.g. a default-printer
  flag or sharing toggle that does not stick), the recurring lesson, so the
  postcondition re-read is mandatory, not optional.
- **Secrets.** No persistent secret is expected on USB/eSATA or local USB
  printers. If live-verify surfaces any credential field (e.g. a network/IPP
  printer auth token or a shared-printer password), it must use the existing
  `credential_ref: env:NAME` mechanism, be resolved only at apply time, and be
  absent from the request, plan, hash, result, and logs. Device serials and
  identifiers are inventory data, not secrets, but are not used as credentials.
- **Read-only gateway.** The read-only MCP gateway excludes every plan/apply
  tool (eject, printer change, spooler clear); only the `get_*` reads are served
  read-only.

## Acceptance criteria

- [ ] Slice A: `external-device capabilities|storage|printers` (CLI) and the
      matching `get_external_device_capabilities` / `get_external_storage` /
      `get_external_printers` MCP tools return normalized state; each area
      selects its own backend and reports `(not supported)` when its API is
      absent without disabling the others.
- [ ] Decoders normalize USB/eSATA device + partition shapes and the printer
      list, and return an error for a malformed response rather than a silent
      empty success.
- [ ] Slice A live-verified on the lab NAS: capability selection and reads for
      whichever external-device families the lab advertises (USB storage at
      minimum), against a throwaway `DSMCTL_DUMP` probe, with the API/version
      names corrected to what the lab actually returns.
- [ ] Slice B: guarded **eject** via hash-bound plan/apply — device-bound
      fingerprint (identity, partitions, mount/busy state), stale rejection,
      typed eject, and a list-based postcondition confirming the device is gone
      or safe-to-remove; classified HIGH; request-capture test asserts the exact
      wire method and key field.
- [ ] Slice B: guarded **printer set** (name/default/manager/sharing) via
      plan/apply with patch-only ownership and a postcondition re-read that
      catches silently-ignored fields; classified medium.
- [ ] Slice B: guarded **spooler clear** via plan/apply, classified high
      (destructive), with a postcondition confirming the queue drained.
- [ ] CLI + MCP wiring complete; the read-only gateway excludes all plan/apply
      tools (`plan_external_device_eject` / `apply_external_device_eject_plan`,
      `plan_external_printer_change` / `apply_external_printer_plan`,
      `plan_external_printer_spooler_clear` / `apply_...`).
- [ ] `docs/control-panel.md` documents the module, the eject/spooler data-safety
      warnings, and the UPS-is-separate boundary.

## Verification

- Decoder + request-capture unit tests; `go test ./... -count=1`,
  `go vet ./...`.
- Live reads on the lab NAS via a throwaway read-only `DSMCTL_DUMP` probe to
  pin down the real API families, versions, and field names before decoders and
  requests are frozen. `SYNO.API.Info` first to confirm which
  `SYNO.Core.ExternalDevice.*` APIs the lab advertises.
- **Live-mutation policy.** Eject, printer set, and spooler clear each require
  explicit per-session authorization **and** the corresponding hardware attached
  (a throwaway USB/eSATA disk to eject, a connected printer with a queued job to
  clear). The lab may have no external device or printer attached; in that case
  the mutating call is **source-verified** (method + key field confirmed against
  the DSM WebAPI conf and handler sources on codesearch, e.g.
  `webapi-Core`/`SYNO.Core.ExternalDevice.*`) rather than live-executed, and the
  surrounding contract (read, capability selection, plan not-found / stale
  paths) is live-verified — the WI-054 precedent for a write with no available
  live target. Do not ship an eject write on source verification alone if a
  disposable USB device can be attached: this module's own family has never been
  live-exercised, and field/method sources have been wrong before.

## Coordination

- Shares the Control Panel facade (`internal/synology/externaldevice.go`
  registered in `internal/runtime/manager.go`) and lives in parallel group C
  alongside the other Control Panel modules (WI-041 external access, WI-012 file
  services, WI-011 time). New operation package under
  `internal/synology/operations/externaldevice`; domain model under
  `internal/domain/externaldevice`. No overlap with those modules beyond the
  shared facade and the shared read-only gateway registration.
- **UPS** is a deliberately separate future WI (Hardware & Power area); flag it
  when that item is scoped so the two do not both claim
  `SYNO.Core.ExternalDevice.*`.
- Distinct from internal storage management (WI-001..003): USB/eSATA disks here
  are external-device inventory, not Storage Pool / volume members.
