---
id: WI-039
title: Redesign the CLI web-login helper page with shared design tokens
status: done
priority: P2
owner: ""
depends_on: [WI-037]
parallel_group: G
touches:
  - internal/weblogin
  - docs/assets/weblogin
  - spec/roadmap.md
---

# WI-039 — Redesign the CLI web-login helper page with shared design tokens

## Outcome

The loopback helper page served by `dsmctl auth login` carries the same visual
identity as the gateway administration UI (brand-blue and slate token scales,
typography, card language) and communicates sign-in progress and outcome at a
glance in the user's browser language. Today the page is unstyled system-ui
text that displays raw server response strings.

## Scope

- Restyle the page produced by `buildPage` in `internal/weblogin/weblogin.go`
  using the documented brand/slate scales and semantic aliases from
  `internal/gateway/admin/ui.go`.
- Centered single-card layout: gateway canvas gradient backdrop, brand mark,
  product title, a target line naming the NAS origin being signed in to,
  status area, primary action button, and two footnote lines
  stating the flow's security facts (password entered only on the NAS's own
  page; PKCE + Noise-encrypted code exchange).
- Four visual states — waiting, exchanging, success, failure — switched by the
  `/callback` HTTP status instead of injecting the server's response text.
  Success shows the semantic success color; failure shows the danger color.
- Localized copy for `en`, `zh-TW`, `zh-CN`, `ja`, `de`, selected once from
  `navigator.language` using the gateway's locale normalization rules;
  `<html lang>` follows the selection. No manual selector.
- Add the standard viewport meta tag.
- Page-contract tests in `internal/weblogin` mirroring the gateway's rendered-UI
  token assertions.

## Non-goals

- No change to the OAuth/PKCE/Noise flow, the loopback server, the `/callback`
  request/response protocol, or server-side response text.
- No window-count reduction, auto-close, or focus management.
- No locale selector and no locale persistence (each run is a distinct
  `127.0.0.1:<random-port>` origin, so storage cannot carry over).
- No external assets, fonts, or requests of any kind from the page.
- No shared design-token Go package (candidate follow-up; would touch the
  gateway's embedded UI). The token block is duplicated with a source-of-truth
  comment pointing at `internal/gateway/admin/ui.go`.
- No gateway enrollment page changes.

## Design constraints

- The page stays one self-contained Go string served from the loopback
  listener: inline CSS/JS only.
- Token values must equal the gateway scales exactly; both packages' tests pin
  the same literals so drift fails a build.
- `loginURL` and `dsmOrigin` embedding remains internally validated string
  interpolation, as today.
- Bright brand blue is reserved for the action button and focus; success,
  warning, and danger keep their semantic colors; content stays on light
  surfaces (per WI-037 constraints).
- The outcome state must be readable at a glance when the user returns from
  the DSM popup window.

## Acceptance criteria

- [x] The rendered page contains the shared brand/slate scales and semantic
      aliases, pinned by a test whose expected values match the gateway's.
- [x] The four states have distinct visuals; the terminal state (success or
      failure) is chosen by the callback HTTP status, and its copy is
      localized, not server text.
- [x] All five locales render through `navigator.language` detection with the
      matching `<html lang>` value.
- [x] `dsmctl auth login` still completes end to end against a live DSM.
- [x] `go test ./... -count=1` and `go vet ./...` pass with the new
      page-contract tests in `internal/weblogin`.
- [x] A screenshot of the redesigned page is versioned under
      `docs/assets/weblogin/`.

## Verification

- `go test ./internal/weblogin -count=1`, then `go test ./... -count=1` and
  `go vet ./...`.
- Serve the page and walk through it in a Chromium-based browser; spot-check
  localization by overriding the browser language.
- Live: run `dsmctl auth login` against a DSM 7.x NAS and confirm the waiting,
  exchanging, and success states. Sign-in only; no DSM mutation is authorized
  or required.

## Coordination

Presentation-only change confined to `internal/weblogin`; no gateway admin
files are touched. WI-038 runs in the same parallel group but touches only
gateway and MCP-server files, so the two can proceed concurrently. WI-017
certifies the gateway image and is unaffected. A
future shared-token package would touch `internal/gateway/admin/ui.go` and
must be its own prerequisite work item.

## Completion notes

- The helper page now carries the gateway token scales verbatim (source of
  truth `internal/gateway/admin/ui.go`); `TestPageCarriesSharedDesignTokens`
  pins the shared literals on this side and the gateway pins the same values,
  so drift fails a build.
- Heading names the target NAS host ("Sign in to <host>", localized with a
  `{host}` placeholder in all five locales because word order differs), with
  the full origin kept below it so scheme and port stay checkable — added
  during live verification at the operator's request.
- Four states (waiting / exchanging / success / error) switch on the
  `/callback` HTTP status; the old server-text injection is removed and its
  absence is test-guarded. Locale comes from `navigator.language` through the
  gateway's normalization rules; `<html lang>` follows.
- Verified: full Playwright walkthrough of all four states and the zh-TW
  locale; live `dsmctl auth login` sign-in completed by the operator against
  DSM 7.3.2; `go test ./... -count=1`, `go vet ./...`, gofmt clean.
- Screenshot versioned at `docs/assets/weblogin/01-sign-in.png` (1280x720,
  zh-TW waiting state).
- Final whole-change review: READY, no Critical/Important findings. Minors
  triaged: package-wide gofmt realignment kept (pure whitespace);
  `role="status"` announcement is AT-dependent (terminal output remains the
  authoritative channel); host/origin splices rely on the loopback self-input
  trust model rather than character sanitization — escaping the splice points
  is a recommended follow-up hardening.

## Handoff

Fill this only when pausing incomplete work.
