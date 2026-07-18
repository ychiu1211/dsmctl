---
id: WI-035
title: Add multilingual MCP Server product copy
status: in_progress
priority: P1
owner: "mcp-product-copy"
depends_on: [WI-033]
parallel_group: G
touches:
  - internal/gateway/admin
  - docs/gateway.md
  - spec/roadmap.md
  - spec/work-items/WI-017-amd64-linux-synology-distribution.md
---

# WI-035 - Add multilingual MCP Server product copy

## Outcome

Users immediately understand that the application is an MCP Server for
managing multiple Synology NAS systems. The complete administration experience
is available in English, Traditional Chinese, Simplified Chinese, Japanese, and
German with concise, professional, task-oriented copy.

## Scope

- Rename the visible product identity to `dsmctl MCP Server` while retaining
  Gateway terminology only where it describes the transport or local trust
  boundary.
- State the `/mcp` endpoint purpose on the overview and MCP access views.
- Rewrite setup, login, overview, navigation context, empty states, and helper
  text using short operational language.
- Add a locale selector before and after authentication for `en`, `zh-TW`,
  `zh-CN`, `ja`, and `de`; initialize from a saved choice or browser language.
- Translate static labels, security guidance, validation feedback, status,
  empty states, confirmations, and dynamically generated table/action copy.
- Persist only the locale identifier in browser-local storage; language choice
  is not authentication state and must not contain credentials.
- Preserve Traditional Chinese labels and the existing embedded, responsive UI.

## Non-goals

- Changing executable names, HTTP paths, APIs, database schema, authentication,
  authorization, deployment artifacts, or DSM behavior.
- Adding marketing copy, product claims, new visual assets, a translation
  service, or a new layout.

## Design constraints

- `MCP Server` must be visible before and after authentication without implying
  that MCP credentials grant DSM or host-NAS authority.
- Text must describe function, state, or required action; avoid emotional,
  promotional, or conversational language.
- The embedded page remains fully offline. Missing translations fall back to
  English and dynamic values must be inserted as text rather than HTML.
- Existing security, accessibility, CSP, and responsive-layout contracts remain
  unchanged.

## Acceptance criteria

- [ ] Setup, login, top bar, and overview visibly identify the product as an
      MCP Server.
- [ ] Overview explains multi-NAS management and exposes the `/mcp` endpoint in
      concise operational language.
- [ ] Existing promotional or conversational copy is replaced with short,
      task-oriented text.
- [ ] English, Traditional Chinese, Simplified Chinese, Japanese, and German
      cover every user-visible administration string, including dynamic states
      and confirmations.
- [ ] Locale defaults from browser preference, can be changed before or after
      login, persists across reload, and does not affect authentication state.
- [ ] Existing actions, endpoints, IDs, confirmations, and security behavior
      remain unchanged.
- [ ] UI tests, full Go tests, vet, Docker build, and desktop/mobile browser
      walkthrough pass without overflow or console errors.
- [ ] User documentation uses the same product terminology.

## Verification

- Extend rendered-UI assertions for MCP Server identity, endpoint copy, all five
  locale catalogs, safe text insertion, and locale persistence.
- Run `go test ./... -count=1`, `go vet ./...`, and `git diff --check`.
- Build and inspect the `linux/amd64` image in an isolated localhost container;
  no NAS connection or DSM mutation is authorized or required.

## Coordination

WI-017 owns distribution and hardware certification. It must certify this final
multilingual copy in the shared image; WI-035 does not change packaging or
runtime behavior.

## Handoff

Fill this only when pausing incomplete work.
