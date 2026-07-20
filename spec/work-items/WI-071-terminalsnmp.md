---
id: WI-071
title: Terminal (SSH/Telnet) and SNMP module
status: proposed
priority: P2
owner: ""
depends_on: [WI-006]
parallel_group: C
touches:
  - internal/domain/terminalsnmp
  - internal/synology/operations/terminalsnmp
  - internal/synology/terminalsnmp.go
  - internal/runtime/manager.go
  - internal/application/terminalsnmp.go
  - internal/cli/terminalsnmp.go
  - internal/mcpserver/server.go
  - docs/control-panel.md
---

# WI-071 — Terminal (SSH/Telnet) and SNMP module

## Outcome

A CLI user or MCP agent can read the Control Panel → Terminal & SNMP surface —
whether SSH and Telnet are enabled and on which SSH port, and the SNMP service
state (enabled versions, device location/contact, whether a community/v3 user
and trap target are configured) — and, through the hash-bound plan/apply
contract, change those settings under guardrails. This is a focused Control
Panel module in the sense of [WI-006](WI-006-control-panel-modules.md): one
typed module per DSM setting area, never a generic `set key=value` proxy. It
mirrors the guarded service-toggle shape already shipped for FTP/SFTP
([WI-027](WI-027-ftp-sftp.md)).

The API map, versions, fields, and set shapes below are the author's best
current knowledge and **must be live-verified at implementation time**. The
standing policy holds: source-doc and mobile-client field names are frequently
stale, so every API family named here is confirmed against the lab with a
throwaway read-only `DSMCTL_DUMP` probe before any decoder or request shape is
trusted (see [[dsm-webapi-live-verify-fields]]).

## Scope

Sliced read-first, then guarded write, so the read slice ships independently.

### Slice A — read-only (independently shippable)

- **Terminal:** `SYNO.Core.Terminal` `get` (family/version **to be
  live-verified**; likely v1–v3) → normalized `{ssh_enabled, ssh_port,
  telnet_enabled}`. Likely wire fields `enable_ssh`, `ssh_port`,
  `enable_telnet` — confirm names and types against the lab.
- **SNMP:** `SYNO.Core.SNMP` `get` (**to be live-verified**) → normalized
  `{snmp_enabled, v1_v2c_enabled, v3_enabled, location, contact,
  community_configured (bool), v3_user, trap_configured (bool),
  trap_host_present (bool)}`. The likely wire fields (`enable_snmp`,
  `snmpv1`/`snmpv2`, `snmpv3`, `snmp_location`, `snmp_contact`,
  `snmpv3_user`, `trap_host`) need name/type confirmation.
- **Secret suppression on read is mandatory.** The SNMP `get` may echo the
  v1/v2c community string, the SNMPv3 auth/privacy passwords, and the trap
  community in cleartext. The decoder **drops these values** and never stores
  them in the state model — reads surface only *presence* (`community_configured`,
  a redacted `v3_user`), never the secret itself (see Design constraints).
- **Capabilities:** each area reports its stable operation name, selected
  backend, API, and version; a NAS missing one family reports `(not supported)`
  without erroring the other.

### Slice B — guarded write (plan/apply, hash-bound)

- **Terminal enable / SSH port / Telnet enable** — `SYNO.Core.Terminal` `set`
  (**to be live-verified**): toggle `enable_ssh`, set `ssh_port`, toggle
  `enable_telnet`. Patch-only merge over a fresh read; unspecified switches
  preserved.
- **SNMP enable / versions / device info** — `SYNO.Core.SNMP` `set`: toggle
  the service, enable/disable v1/v2c and v3, set `location` and `contact`.
- **SNMP community and SNMPv3 credentials** — the v1/v2c community string and
  the SNMPv3 auth/privacy passwords (and any trap community) are supplied as
  `credential_ref: env:NAME`, resolved only at apply time (see Secrets below).
- **SNMP trap target** — set the trap host/community for the configured
  version. (Advanced trap semantics deferred — see Non-goals.)

## Non-goals

- **Firewall and Auto Block.** `SYNO.Core.Security.Firewall` rules and
  `SYNO.Core.SecurityAutoBlock` are separate Security-panel API families and
  separate work items. This WI does not open, close, or reconcile a firewall
  rule for the SSH port even though a port change interacts with one (called out
  as a lockout hazard in Design constraints).
- **SSH key / host-key management and per-user shell access.** Public-key auth
  configuration, host-key rotation, and granting a user a login shell are not on
  the Control Panel Terminal tab and are out of scope.
- **Advanced SNMP trap modes.** SNMP *inform* vs *trap*, custom engine IDs, and
  multiple trap destinations are deferred; Slice B configures a single trap
  target only, added the same patch-only way once each value set is confirmed.
- **SNMPv3 protocol menu beyond the common set.** The initial write supports the
  standard auth (MD5/SHA) and privacy (DES/AES) selections DSM exposes; less
  common protocols are added later after live confirmation of the permitted
  value set.
- **Any generic raw `SYNO.Core.Terminal`/`SYNO.Core.SNMP` mutation passthrough.**
  Prohibited by the mutation-safety contract; only the typed operations above.

## Design constraints

- **Independent compatibility boundaries.** Terminal and SNMP are distinct API
  families with distinct failure boundaries. Per the compatibility contract,
  selection is per operation: SNMP being absent or the service package
  unavailable must not disable the Terminal read/write, and vice versa. Each
  reports `(not supported)` and fails **closed** (no silent empty-success
  decode) when its API is missing.
- **Every write is high risk — it changes the attack surface.** Enabling SSH,
  enabling Telnet, or changing the SSH port all widen (or move) remote
  management exposure; enabling SNMP opens a queryable management channel.
  Classify **all** Slice-B mutations high. There is no "medium" toggle here.
  Telnet especially is unauthenticated-cleartext and deprecated — enabling it is
  high risk and the plan summary must say so explicitly.
- **SSH-port change is a lockout hazard.** Changing `ssh_port` while a firewall
  rule or an upstream port-forward still pins the old port can strand human
  admin access; disabling SSH drops any admin relying on it. dsmctl itself
  drives DSM over the WebAPI session (not SSH), so its own connectivity survives,
  but the plan must warn that the *human* SSH/firewall path may break and that
  the caller is responsible for the matching firewall change (out of scope
  here). Treat SSH-service disable and port change as privilege-affecting,
  high-risk, and keep the change reversible.
- **Secrets never enter requests/plans/hashes/logs/MCP args.** The SNMP v1/v2c
  community string, the SNMPv3 auth and privacy passwords, and any trap
  community are secrets under the secrets-and-identity contract, handled exactly
  like the user-password change: passed as `credential_ref: env:NAME`, resolved
  at apply time, and provably absent from the request the plan records, the
  approval hash, the result, the logs, and every MCP tool argument. On the read
  side the decoder must **discard** any secret DSM echoes back so reads never
  leak them either.
- **Patch + postcondition.** Follow the module pattern: plan records and hashes
  the complete observed state (with secrets excluded), apply rejects a changed
  state, merges the patch into a freshly read config, and **re-reads** to verify
  the requested fields took effect — DSM silently ignores some fields (the
  recurring lesson from FTP/SFTP and NFS advanced). Because secret values are
  suppressed on read, the postcondition verifies *effect* (e.g. "v3 user now
  present", "SSH now enabled on port N"), not the secret bytes.
- **Set-shape symmetry is unconfirmed.** As in FTP (which requires both
  `enable_ftp`+`enable_ftps` present) and NFS advanced (int-vs-bool re-encoding),
  the required-field set and get/set type symmetry for both `SYNO.Core.Terminal`
  and `SYNO.Core.SNMP` `set` must be confirmed live before Slice B ships; the
  facade resends whatever the setter requires (likely the full enable triple for
  Terminal, the version flags + device info for SNMP) merged from the fresh read.
- **Read-only gateway exclusion.** The remote/read-only gateway must exclude the
  `plan_terminal_snmp_change` / `apply_terminal_snmp_plan` tools (and the CLI
  set path) exactly as the other guarded modules do; the tool-count test asserts
  the exclusion.

## Acceptance criteria

- [ ] Slice A: `control-panel terminal-snmp capabilities|terminal|snmp` (CLI)
      and the matching `get_terminal_snmp_*` MCP tools return normalized state;
      the SNMP community string and SNMPv3 passwords are provably absent from all
      output (unit test asserts the decoded state never carries them; live
      `--json` grep confirms no secret leak).
- [ ] Terminal and SNMP each select their own backend and report a stable
      operation name / API / version in the capability report; a missing family
      reports `(not supported)` and does not disable the other.
- [ ] Decoders use strict, required-field validation and return an error for
      malformed shapes (no silent empty-success), matching the compatibility
      contract.
- [ ] Slice A live verification on the DSM lab: read Terminal (SSH state + port,
      Telnet state) and SNMP (service state, versions, location/contact, whether
      community/v3/trap are configured) with no secret leak.
- [ ] Slice B: Terminal `set` (SSH enable, SSH port, Telnet enable) and SNMP
      `set` (enable, versions, location/contact, trap target) via guarded
      hash-bound plan/apply, with request-capture tests and a postcondition
      re-read; all classified high risk; the read-only gateway excludes the
      plan/apply tools.
- [ ] `credential_ref: env:NAME` supplies the SNMP community and SNMPv3
      auth/privacy passwords; a request-capture test proves the resolved secret
      appears only in the wire request and never in the plan, approval hash,
      result, or logs.
- [ ] Plan summaries name the exposure change explicitly (e.g. "enables Telnet
      (cleartext, deprecated)", "moves SSH to port N — verify firewall/port
      forward separately") and mark every mutation high risk.
- [ ] Slice B live verification on the DSM lab (explicitly authorized, fully
      reverted): an SSH-port round-trip (22 → temp → 22) and an SNMP
      enable/disable round-trip through plan/apply with postcondition proof,
      performed so the WebAPI management session is never lost.

## Verification

- Decoder fixtures + request-capture unit tests per family (Terminal, SNMP),
  including the credential_ref redaction assertion; `go test ./... -count=1`,
  `go vet ./...`, CLI and MCP builds.
- Live read now (read-only probe via `DSMCTL_DUMP` to confirm the real
  `SYNO.Core.Terminal` / `SYNO.Core.SNMP` families, versions, field names, and
  types before writing decoders).
- Live reverted write requires explicit per-session authorization. Because SSH
  port/enable and Telnet changes are lockout- and exposure-affecting, the live
  apply is done carefully with the change kept reversible and the WebAPI session
  preserved; **do not** live-enable Telnet on any shared NAS beyond the momentary
  reverted round-trip.
- Source of truth for fields to reconcile against the lab: the `webapi-Terminal`
  and `webapi-SNMP` repos (conf + `src/`) on codesearch, branch matching the lab
  DSM release — treated as a lead, not authority, per the live-verify policy.

## Coordination

- Shares `internal/domain/controlpanel` / `internal/synology/controlpanel.go`
  and the compatibility report with the other Control Panel modules (parallel
  group C, all `depends_on: [WI-006]`). New operation package under
  `internal/synology/operations/terminalsnmp`; the only cross-file overlap is
  `internal/mcpserver/server.go` (tool registration + read-only gate + tool-count
  test) and `internal/runtime/manager.go`, as in [WI-041](WI-041-external-access.md)
  and [WI-027](WI-027-ftp-sftp.md).
- Interacts with (but does not touch) a future Security/Firewall module: an
  SSH-port change should eventually be cross-referenced with the firewall rule
  managing that port; that reconciliation is out of scope here and belongs to the
  firewall work item.

## Handoff

Fill this only when pausing incomplete work.
