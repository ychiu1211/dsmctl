---
id: WI-107
title: Preserve interactive approval detection across the MCP SDK adapter
status: done
priority: P1
owner: ""
depends_on: [WI-033]
parallel_group: G
touches:
  - internal/mcpserver/remote_policy.go
  - internal/mcpserver/remote_policy_test.go
---

# WI-107 — Preserve interactive approval detection across the MCP SDK adapter

## Outcome

Interactive-approval credentials reliably receive a session-bound MCP form
elicitation when a high-risk apply reports the internal exact-plan approval
sentinel, even after the SDK tool adapter reconstructs the result and loses the
in-process error identity.

## Scope

- Prefer typed sentinel matching when the SDK preserves the error.
- Fall back only to an exact full-text match of the internal sentinel in an
  error result.
- Add an SDK marshal/unmarshal regression test and retain the end-to-end
  stateful-session test.

## Acceptance criteria

- [x] Typed and SDK-reconstructed approval-required results are detected.
- [x] Other error text, including substrings containing the sentinel, is not
      accepted.
- [x] Conversation accept/decline/cancel/unsupported-client tests pass.
- [x] `go test ./...` and `go vet ./...` pass.

## Verification

- `go test ./internal/mcpserver ./internal/gateway -count=1`
- `go test ./... -count=1`
- `go vet ./...`

## Handoff

- The full regression gate exposed a deterministic failure: the MCP SDK tool
  adapter preserved `IsError` and exact content but not `CallToolResult.err`,
  so `errors.Is(result.GetError(), sentinel)` never initiated elicitation.
- Detection now prefers the typed sentinel and uses an exact full-text fallback
  only for SDK-reconstructed error results. Prefixes, suffixes, substrings, and
  non-error results remain rejected.
- The SDK adaptation regression test and the end-to-end accept, decline,
  cancel, unchecked, and unsupported-client cases pass. The test was repeated
  three times before the full suite and `go vet` passed.
- The fix is included in the healthy deployed `7.3.2-34` Gateway image.
