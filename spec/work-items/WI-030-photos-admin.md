---
id: WI-030
title: Synology Photos administration module
status: done
priority: P2
owner: ""
depends_on: [WI-019, WI-022]
parallel_group: C
touches:
  - internal/domain/photos
  - internal/synology/operations/photos
  - internal/synology/photos.go
  - internal/application/photos.go
  - internal/cli/photo.go
  - internal/mcpserver/server.go
  - docs/packages.md
---

# WI-030 — Synology Photos administration module

## Outcome

A CLI user or MCP agent can read the Synology Photos administration settings and,
through a hash-bound plan/apply, change them — mirroring the Drive Admin module
(WI-022), package-version gated on the installed SynologyPhotos package.

## Scope

- Read `SYNO.Foto.Setting.Admin` (get v1): face/concept/similar grouping, user
  sharing, guest info visibility, personal/shared recycle bins, converted
  original JPEG, HEVC requirement, default thumbnail size, excluded extensions,
  and the read-only package version.
- Guarded partial write (`set` v1): only the changed fields are sent; unspecified
  settings are preserved. Postcondition re-read verifies the requested change
  actually took effect.
- Package-version gating on `SynologyPhotos` (>= 1.0, verified on 1.9.1) so a NAS
  without Photos (or with an untested older version) fails closed with evidence.

## Non-goals

- End-user photo/album browsing and upload (`SYNO.Foto.Browse.*`), background
  tasks, and per-user space management.
- Shared-space folder creation and per-user permissions.

## Design constraints

- DSM field names confirmed live on Synology Photos 1.9.1 (via a temporary probe,
  since no public schema): `enable_person`, `enable_concept`, `enable_similar`,
  `enable_user_sharing`, `display_photo_info_to_guest`,
  `enable_personal_dsm_recycle_bin`, `enable_shared_dsm_recycle_bin`,
  `enable_converted_original_jpeg`, `need_hevc`, `default_thumbnail_size`,
  `exclude_extension`, `package_version` (read-only).
- The set is a partial update; the postcondition check catches fields that DSM
  accepts but does not actually change (e.g. `enable_converted_original_jpeg`,
  which is conditional), failing the apply rather than reporting a false success.
- Disabling a recycle bin (deletions become permanent) or user sharing (revokes
  existing shares), or disabling face recognition, is high risk.

## Acceptance criteria

- [x] Admin settings decode with semantic names; the decoder requires the stable
      `enable_person` field to catch API drift and tolerates version-specific
      extras.
- [x] Package-version gating: read/set fail closed without SynologyPhotos, with
      package evidence in capabilities and read errors.
- [x] Guarded partial write with request-capture test and postcondition
      verification.
- [x] CLI (`photo capabilities|settings|plan|apply`) and four MCP tools
      (`get_photos_capabilities`, `get_photos_settings`, `plan_photos_change`,
      `apply_photos_plan`) with read-only-gateway exclusion of plan/apply.
- [x] DSM 7.3.2 live verification (lab, authorized, fully reverted): read the
      settings, toggled `show_info_to_guest` true→false→true through plan/apply;
      the postcondition correctly rejected a `converted_original_jpeg` toggle that
      DSM does not apply standalone.

## Verification

- Decoder + request-capture tests; `go test ./... -count=1`, `go vet ./...`.
- Live read and reverted write on the DSM 7.3.2 lab NAS (Synology Photos 1.9.1).
