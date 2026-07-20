---
id: WI-055
title: Drive user-privilege view (read) and the access-control finding
status: done
priority: P2
owner: ""
depends_on: [WI-022, WI-053]
parallel_group: C
touches:
  - internal/domain/driveadmin
  - internal/synology/operations/driveadmin
  - internal/synology/driveadmin.go
  - internal/application/drive_admin.go
  - internal/cli/drive.go
  - internal/mcpserver/server.go
  - docs/drive-admin.md
---

# WI-055 — Drive user-privilege view (read) and the access-control finding

## Outcome

A read of the Admin Console user view: which accounts may use Drive and
whether Drive has materialized each account — plus the live-verified finding
that fixes how dsmctl models "who can use Drive": the DSM **application
privilege** (already managed by the account module) is the control, not
Drive's own Privilege API.

## Scope

- `SYNO.SynologyDrive.Privilege` `list` v1 with `additional:
  ["enabled","status"]` (a bare boolean is rejected with 120, verified live):
  per-account `name`, `enabled`, and `status` (`normal`, `disabled` for a
  deactivated DSM account, `home_disabled`). Realms: local (default), domain,
  ldap with `domain_name`.
- CLI `drive admin users [--type local|domain|ldap] [--domain-name …]`; MCP
  `get_drive_users`.

## The finding (live-verified, 2026-07-20)

Using a disposable `dsmctl-e2e-*` account on the lab target:

1. The privilege view lists exactly the accounts the **DSM application
   privilege** (`SYNO.SDS.Drive.Application`) allows: denying it through the
   account module removed the account from the view immediately.
2. `enabled` reports whether Drive materialized the account's user row: it
   flipped true shortly after the account was granted access and had logged
   in once (view creation is asynchronous and logged as a Drive event).
3. Drive's own `Privilege.set` (enable/disable user rows) **does not stick**
   while the application privilege still allows the account — repeated
   disables were absorbed with no state change (dsmctl's postcondition
   correctly refused to report success). It is therefore deliberately **not
   exposed** as a dsmctl write: granting or revoking Drive access is an
   account-module `application_privilege` change, which is guarded, live-
   verified, and already shipped.

## Acceptance criteria

- [x] Privilege view read with strict decoding (missing `enabled` is an
      explicit error naming the missing additional fields), CLI, and MCP.
- [x] Live verification on Drive 4.0.3-27892: realm listing, additional
      fields, app-privilege deny/grant interplay; disposable account removed
      afterwards.
- [x] Documentation points Drive access control at the account module and
      records why Privilege.set is not exposed.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live reads and the app-privilege experiment on the DSM 7.3.2 lab NAS.
