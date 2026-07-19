---
id: WI-040
title: Design and apply the dsmctl favicon
status: done
priority: P2
owner: ""
depends_on: [WI-037, WI-039]
parallel_group: G
touches:
  - internal/webassets
  - internal/gateway/admin
  - internal/weblogin
  - spec/roadmap.md
---

# WI-040 – Design and apply the dsmctl favicon

## Outcome

The dsmctl browser surfaces use one recognizable favicon derived from the
existing four-tile brand mark. It stays legible at browser-tab sizes, uses the
shared brand-blue palette, and is served locally by both the Gateway Admin UI
and the `auth login` loopback helper.

## Scope

- Create a reusable, repository-native SVG favicon with a compact rounded
  brand-blue field and the existing two-by-two white tile mark.
- Keep the SVG source in a shared internal web-assets package so both browser
  surfaces return identical bytes.
- Add an explicit same-origin favicon link and matching browser theme color to
  the Gateway Admin UI.
- Add the same favicon and theme color to the CLI web-login helper page.
- Add response and rendered-page contract tests for the asset, content type,
  links, theme color, and offline-only behavior.

## Non-goals

- No Synology SPK, DSM package, desktop application, CLI, or documentation-site
  icon changes.
- No external fonts, image requests, CDN assets, generated bitmap variants, or
  animated icon.
- No changes to authentication, authorization, OAuth/PKCE/Noise, gateway MCP,
  or NAS operation behavior.

## Design constraints

- The mark must remain distinct at 16x16 CSS pixels and must not rely on text,
  hairline strokes, or fine details.
- Colors come from the WI-037 brand scale: `#4da5f4`, `#2588df`, and
  `#146fbd`; browser chrome uses the existing slate/brand surface theme.
- Both favicon endpoints are same-origin and require no network dependency.
- Existing CSP and the loopback helper's offline-only page contract remain
  intact.

## Acceptance criteria

- [x] One shared SVG source renders the established four-tile dsmctl mark and
      is returned byte-for-byte by both browser surfaces.
- [x] Admin UI and web-login HTML declare the SVG favicon and a matching theme
      color using same-origin URLs.
- [x] Favicon responses use `image/svg+xml`, do not expose credentials, and do
      not interfere with existing page, API, or callback routes.
- [x] Focused package tests, `go test ./... -count=1`, and `go vet ./...` pass.
- [x] The SVG receives a visual small-size inspection before completion.

## Verification

- `go test ./internal/webassets ./internal/gateway/admin ./internal/weblogin -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- Render and inspect the source SVG at normal and favicon-scale dimensions.

## Coordination

WI-017 and WI-029 remain in progress. This item does not touch package
distribution or DSM package-management code. If the Synology SPK later needs a
Package Center icon, treat that as separate WI-017 packaging work with the
platform-required bitmap sizes rather than coupling it to this browser asset.

## Completion notes

- Added one embedded SVG source in `internal/webassets`: a rounded brand-blue
  gradient using the WI-037 palette with four white 16-unit tiles aligned to
  the 64-unit view box for crisp 16px scaling.
- Gateway Admin serves it at `/admin/favicon.svg`; the CLI web-login helper
  serves the identical bytes at `/favicon.svg`. Both pages declare the icon,
  `sizes="any"`, and the shared `#0d263f` browser theme color.
- The shared handler limits methods to GET/HEAD and returns SVG, nosniff,
  same-origin-safe CSP, and cache headers. Page tests continue to reject
  network-loaded assets.
- Visually rendered in Edge at 16, 32, 64, and 128 CSS pixels on light and dark
  backgrounds; the four-tile silhouette remained distinct at 16px.
- Verified with focused package tests, `go test ./... -count=1`, `go vet ./...`,
  and `git diff --check`. The local Go telemetry warning was non-fatal; all
  commands exited successfully using a workspace-local build cache.

## Handoff

Fill this only when pausing incomplete work.
