---
id: WI-067
title: Account protection and auto-block
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/accountprotection
  - internal/synology/operations/accountprotection
  - internal/synology/accountprotection.go
  - internal/runtime/manager.go
  - internal/application/accountprotection.go
  - internal/cli/accountprotection.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-067 — Account protection and auto-block

## Outcome

A CLI user or MCP agent can read the Control Panel → Security → Account surface —
Auto Block (settings plus the allow/block IP lists), Account Protection, DoS
protection, and the domain-wide enforced-2FA/MFA policy — and, through the
hash-bound plan/apply contract, change those settings under guardrails. This is a
focused Control Panel module in the sense of
[WI-006](WI-006-control-panel-modules.md): one typed module per DSM setting area,
never a generic `set key=value` proxy. The module never touches per-user OTP
secrets or recovery codes; it manages *policy*, not enrollment.

The API map, versions, read shapes, and set fields below are the author's best
current knowledge and **must be live-verified at implementation time**: the
standing policy is that source-doc / mobile-client field and method names are
often stale, so each family must be confirmed against the lab with a throwaway
read-only `DSMCTL_DUMP` probe before any decoder or request builder trusts it
(see [[dsm-webapi-live-verify-fields]]). The likely families are
`SYNO.Core.Security.AutoBlock`, `SYNO.Core.Security.AutoBlock.Rule`,
`SYNO.Core.Security.DoS`, and the enforced-2FA policy family (candidate
`SYNO.Core.OTP.EnforcePolicy`; may instead live under
`SYNO.Core.SecuritySetting` / a `SYNO.Core.User` policy call — resolve at
impl time).

## Scope

Sliced read-first, then guarded write, so the read slice can ship independently.

### Slice A — read-only (independently shippable)

- **Auto Block settings** — candidate `SYNO.Core.Security.AutoBlock` `get` →
  normalized `{enabled, attempts, within_minutes, expire_enabled, expire_days}`
  (DSM's "block after N failed attempts within M minutes; expire block after D
  days"). Exact field names (`attempts`, `within_mins` vs `within`, `expire_day`
  vs `expired_day`, the separate expire-enable flag) are to be live-verified.
- **Auto Block allow / block lists** — candidate
  `SYNO.Core.Security.AutoBlock.Rule` `list` (paged, `offset`/`limit`) →
  normalized entries `{ip_or_subnet, kind: allow|block, record_time, reason}`.
  The list/allow-vs-deny discriminator (`iptype` / `deny` / a `type` enum) is to
  be live-verified.
- **Account Protection** — the "protect accounts by blocking untrusted clients
  after repeated failed sign-ins" thresholds. Candidate
  `SYNO.Core.Security.AutoBlock` extended fields or a sibling
  `SYNO.Core.Security.AccountProtection` `get` →
  `{enabled, untrust_attempts, untrust_within_minutes, trust_attempts,
  trust_within_minutes}`. Whether this is a distinct API or extra AutoBlock
  fields is explicitly to be live-verified.
- **DoS protection** — candidate `SYNO.Core.Security.DoS` `get` →
  `{enabled}` (may be per network interface: a list of `{ifname, enabled}` — to
  be live-verified).
- **Enforced 2FA/MFA policy** — candidate `SYNO.Core.OTP.EnforcePolicy` `get`
  (or the SecuritySetting equivalent) → `{enforce_scope: none|all|groups,
  enforced_groups}`. Read only surfaces the policy; it never reads any user's OTP
  secret, seed, or recovery codes.

### Slice B — guarded write (plan/apply, hash-bound)

- **Auto Block settings** — `enabled`, `attempts`, `within_minutes`,
  `expire_enabled`, `expire_days` via the AutoBlock `set`.
- **Auto Block allow/block list edits** — add / remove entries keyed by
  `ip_or_subnet` + `kind` via the AutoBlock.Rule `add` / `remove` (method names
  to be live-verified). Patch-only add/remove of named entries — never a full
  list replace that could silently drop existing rules.
- **Account Protection** thresholds via its `set`.
- **DoS protection** enable/disable via `SYNO.Core.Security.DoS` `set`.
- **Enforced-2FA policy** — set `enforce_scope` / `enforced_groups` via the
  policy `set`.

## Non-goals

- **Firewall rules** (`SYNO.Core.Security.Firewall.*`). A separate Control Panel
  subsystem with its own allow/deny model; owned by WI-066. Auto Block's IP list
  is a distinct DSM feature from the firewall and is *not* a firewall-rule proxy.
- **Certificate management** (`SYNO.Core.Certificate.*`) — WI-065.
- **Security Advisor** scans, schedules, and results
  (`SYNO.Core.SecurityScan.*`) — WI-068.
- **Per-user 2FA enrollment and OTP lifecycle** — generating/resetting a user's
  OTP secret, backup/recovery codes, or hardware-key registration. These carry
  secrets and are account-lifecycle actions, not a policy patch; out of scope.
- **Trusted-device / credential rotation** — owned by WI-009 (credential
  lifecycle). This module reads and sets the *enforcement policy*, not the
  trusted-device store.
- **Password strength / expiry policy** and login-portal / DSM-port hardening
  (`SYNO.Core.Web.DSM`) — the latter is WI-070.
- **Unblocking a specific already-blocked source as a one-off remediation** is
  expressed only through the guarded allow-list add / block-list remove path
  above; there is no separate un-audited "clear block" fast path.

## Design constraints

- **Independent compatibility boundaries.** Auto Block settings, the AutoBlock
  allow/block list, Account Protection, DoS, and the enforced-2FA policy are
  separate API families and separate failure boundaries. A NAS missing one (a
  model or DSM train without DoS, or an enforce-2FA family under a different API)
  must leave the others usable, reported `(not supported)` via capability
  reporting, and must fail closed for that area rather than erroring the whole
  module. Each area selects its own backend per operation
  (see [architecture-contracts.md](../architecture-contracts.md) → Compatibility).
- **Admin self-lockout guardrail (the defining risk).** The architecture
  contract requires an explicit protection policy for the current-session
  principal. Every Slice-B write here can lock the operator out:
  - Adding the current session's own source IP (or a subnet containing it) to
    the **block** list, or removing it from an **allow** list, must be detected
    and **refused by default**, requiring an explicit, logged override in the
    plan intent.
  - Setting **enforced 2FA** to a scope that includes the current admin (or a
    group they belong to) while that admin has no OTP enrolled must be flagged
    high risk and refused by default with the same explicit-override gate,
    because it can make the admin unable to sign in.
  - Tightening Auto Block thresholds (fewer attempts / longer expiry) that would
    plausibly trap an active session is surfaced in the plan summary.
  The plan records enough observed state (current source IP, whether the admin
  has 2FA, existing list membership) to evaluate these at apply time; apply
  re-reads and re-checks before performing the operation.
- **Every write is high risk.** Broadening blocks (block-list add, stricter
  thresholds, DoS on, enforce-2FA on) can lock out legitimate users; loosening
  them (disable Auto Block, disable DoS, drop to `enforce_scope: none`, allow-list
  a wide subnet) reduces the security posture. There is no "medium" toggle in
  this module — classify all Slice-B mutations **high**.
- **Patch + postcondition.** Follow the module pattern: plan records and hashes
  the complete current state, apply rejects a changed state, merges the patch
  into a freshly read config, performs the typed operation, and **re-reads to
  verify** the requested fields actually took effect. DSM silently ignores some
  fields (the recurring lesson across this project); the postcondition re-read is
  mandatory, especially for the enforce-2FA scope and the DoS per-interface flag.
- **List ownership semantics are explicit and patch-only.** Allow/block list
  edits are add/remove of individually named entries keyed by `ip_or_subnet` +
  `kind`; the module never sends a whole-list payload that could silently reset
  unspecified entries. The plan states this ownership explicitly.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** This module is
  policy-only and normally carries no secret, but the boundary still holds: no
  OTP secret, seed, recovery code, SID, or SynoToken is ever read into a display
  model, plan, hash, result, or MCP argument. Should any set call require a
  confirming credential, it uses the existing `credential_ref: env:NAME`
  mechanism resolved at apply time.
- **Read-only gateway exclusion.** The plan/apply tools are excluded from the
  read-only gateway surface; only the `get_*` reads are exposed there.

## Acceptance criteria

- [ ] Slice A: `account-protection capabilities|autoblock|autoblock-list|dos|
      enforce-2fa` (CLI) and the matching `get_*` MCP tools return normalized
      state; each area independently reports `(not supported)` when its API is
      absent without disabling the others.
- [ ] Decoders normalize each family and return errors for malformed shapes;
      they never silently return an empty successful state (per the Compatibility
      contract).
- [ ] Slice A live verification on the lab against a throwaway read-only probe:
      Auto Block settings, the allow/block list, Account Protection thresholds,
      DoS state, and the enforced-2FA scope all read with correct field mapping;
      the actual DSM field/method names are confirmed and recorded in the API map
      note (corrections captured where the source-doc names were stale).
- [ ] Capability report lists every operation with a stable operation name,
      selected backend, API, and version.
- [ ] Slice B: Auto Block settings, allow/block list add/remove, Account
      Protection thresholds, DoS toggle, and enforced-2FA scope each go through
      guarded hash-bound plan/apply with a request-capture test and a
      postcondition re-read; all classified high risk; read-only gateway excludes
      the plan/apply tools.
- [ ] The admin self-lockout guardrail is enforced and unit-tested: a plan that
      would block the current source IP (or a containing subnet), remove it from
      the allow list, or enforce 2FA over an un-enrolled current admin is refused
      by default and only proceeds with an explicit override recorded in the plan
      intent.
- [ ] Allow/block list edits are proven patch-only: a request-capture test shows
      add/remove of a single entry never sends a full-list payload and never
      resets sibling entries.
- [ ] No secret leak: unit test asserts no OTP secret/recovery material and no
      SID/SynoToken appears in any display model, plan, hash, or MCP argument;
      a live `--json` grep confirms it on the read path.
- [ ] Slice B live verification on the lab (authorized, fully reverted): at
      minimum a DoS off→on→off round-trip and one Auto Block threshold change,
      each through plan/apply with postcondition proof, from a source IP that is
      *not* on the block list. The enforce-2FA and block-list writes are verified
      only under explicit per-session authorization because they can lock the
      operator out.

## Verification

- Decoder + request-capture unit tests, including the self-lockout guardrail and
  the patch-only list semantics; `go test ./... -count=1`, `go vet ./...`.
- Live read now (authenticated session reads via a throwaway `DSMCTL_DUMP` probe
  to confirm field/method names before trusting the source-doc guesses).
- Live reverted write requires explicit per-session authorization. The
  block-list and enforce-2FA writes require the operator to confirm the source IP
  is safe and (for enforce-2FA) that a recovery path exists, given the
  self-lockout risk.
- Source of truth for fields (to confirm on the lab, branch matching the lab DSM
  train): the `SYNO.Core.Security.AutoBlock`, `SYNO.Core.Security.AutoBlock.Rule`,
  and `SYNO.Core.Security.DoS` WebAPI conf + C++ handlers, and the enforced-2FA
  policy handler (codesearch `webapi-Security` / `webapi-OTP`), cross-checked
  against a live dump because these names have been wrong before.

## Coordination

- Group C. New operation package under
  `internal/synology/operations/accountprotection`, domain model in
  `internal/domain/accountprotection`, facade `internal/synology/accountprotection.go`
  registered in `internal/runtime/manager.go`, application layer in
  `internal/application/accountprotection.go`, thin CLI in
  `internal/cli/accountprotection.go`, and thin MCP tools in
  `internal/mcpserver/server.go`. No overlap with the file-service, time, or
  external-access modules beyond the shared facade registration.
- Adjacent security items in the same greenfield group: WI-065 (certificates),
  WI-066 (firewall), WI-068 (Security Advisor), WI-070 (login portal). The Auto
  Block IP list here is a *distinct* DSM subsystem from WI-066's firewall
  allow/deny rules — keep them in separate modules and do not let one proxy the
  other. The enforced-2FA policy read/write here is policy only; per-user 2FA and
  trusted-device management stay in WI-009.
