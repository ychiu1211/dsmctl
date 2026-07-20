---
id: WI-055
title: My Drive (home) versioning write
status: done
priority: P3
owner: ""
depends_on: [WI-050]
parallel_group: C
touches:
  - internal/application/drive_admin.go
  - docs/drive-admin.md
---

# WI-055 — My Drive (home) versioning write

## Outcome

The WI-050 team-folder write now also covers the Drive home entry
(`homes/mydrive_home`): `set_versioning` patches the global My Drive
versioning that DSM fans out to every user home. Enable/disable stay
rejected for the home entry — My Drive activation follows the DSM home
service, not the team-folder switch.

## Scope

- Relax the WI-050 name guard: `set_versioning` on a `homes/*` entry is
  allowed; `enable`/`disable` keep an explicit error.
- Home-entry plans are always **high risk** with an explicit fan-out warning
  (every user home is affected), on top of the usual pruning classification.
- Backend unchanged: the same `SYNO.SynologyDrive.Share` `set` config-only
  entry routes to the handler's home path (`HomeViewSet`), which merges
  omitted fields and applies the setting to each user view.

## Acceptance criteria

- [x] Validation allows home `set_versioning`, rejects home enable/disable.
- [x] Home plans are always high risk with the fan-out warning.
- [x] Live verification on Drive 4.0.3-27892 (2026-07-20): raised the home
      versioning 8→10 through plan/apply with postcondition confirmation,
      then reverted 10→8; the non-destructive direction was chosen so no
      stored versions were pruned.

## Verification

- `go test ./... -count=1`, `go vet ./...`.
- Live 8→10→8 cycle on the DSM 7.3.2 lab NAS, fully reverted.
