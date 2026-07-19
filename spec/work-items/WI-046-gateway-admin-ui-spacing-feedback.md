---
id: WI-046
title: Fix Gateway Admin UI spacing and feedback
status: done
priority: P2
owner: ""
depends_on: [WI-038]
parallel_group: G
touches:
  - internal/gateway/admin/ui.go
  - internal/gateway/admin/handler_test.go
  - spec/roadmap.md
---

# WI-046 — Fix Gateway Admin UI spacing and feedback

## Outcome

The authenticated Admin UI uses consistent vertical rhythm: side-by-side
cards align at the top, contextual notices do not touch action buttons,
password fields have an intentional grouping, and feedback messages remain
visible without indefinitely covering page content.

## Scope

- Keep grid-owned card spacing independent from stacked panel spacing.
- Add a standard gap between a button row and a following contextual notice.
- Group current, new, and confirmation password fields deliberately in the
  two-column administrator form.
- Auto-dismiss successful feedback and make persistent error feedback
  explicitly dismissible and keyboard accessible.
- Add rendered-UI regression assertions for the layout and feedback markers.

## Non-goals

- No API, authentication, token, approval, NAS, or DSM behavior changes.
- No new frontend framework, external asset, design-token overhaul, or copy
  redesign.
- No live NAS calls or mutations.

## Design constraints

- Preserve existing stable control IDs, CSP compatibility, offline rendering,
  and all five locales.
- Keep feedback available to assistive technology and avoid focus traps.
- Grid layouts use `gap`; sibling margins are reserved for vertically stacked
  panels outside a grid.

## Acceptance criteria

- [x] Both cards in every desktop `.content-grid` share the same top edge, and
      the one-column layout has exactly one standard inter-card gap.
- [x] A visible `profileNextStep` has 16px between it and the preceding button
      row.
- [x] The current-password field spans the form width, with new and confirm
      fields paired below it on desktop and all fields stacked on narrow screens.
- [x] Success feedback auto-dismisses; errors remain until dismissed through a
      visible, localized, keyboard-accessible control.
- [x] Focused Admin UI tests, `go test ./...`, `go vet ./...`, browser layout
      measurements, and `git diff --check` pass.

## Verification

- Run `go test ./internal/gateway/admin -count=1`.
- Run `go test ./...` and `go vet ./...`.
- Reload the local Admin UI and inspect all six views at the existing desktop
  viewport; verify exact grid and notice gaps from computed layout.
- No live NAS call or mutation is authorized or required.

## Coordination

WI-017 is active in the same parallel group and owns distribution
certification. This item changes only the embedded Admin UI and its focused
tests; it does not edit packaging or certification documents. WI-017 should
certify the corrected UI after this item lands.

## Completion notes

- Scoped stacked-panel spacing to direct view children, leaving `.content-grid`
  to own its 18px gap. Browser measurements show both Overview cards at
  `top=499.90625px` and both Administrator cards at `top=165.9375px`.
- Added a 16px adjacent-sibling gap before the post-create NAS next-step notice.
- Made the current-password field span both columns and paired new/confirmation
  fields below it; the existing narrow breakpoint continues to stack all fields.
- Replaced the indefinitely obstructing feedback box with a localized close
  button, a four-second success timeout, and persistent dismissible errors.
- Verified the focused Admin UI package, `go test ./...`, `go vet ./...`,
  rendered locale diagnostics, error persistence/dismissal, and whitespace.
  No live NAS call or mutation was performed.

## Handoff

Fill this only when pausing incomplete work.
