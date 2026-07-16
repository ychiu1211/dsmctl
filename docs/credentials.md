# Credential lifecycle

dsmctl stores two secrets per NAS profile in the operating system's
credential store (Windows Credential Manager, macOS Keychain, or Linux
Secret Service): the account password and the DSM trusted-device credential
issued after an OTP login. This guide covers inspecting, removing, and
rotating them. No command in this guide ever prints a secret value.

## Status

```console
dsmctl auth status
dsmctl auth status --nas office --json
```

The table reports, per profile, whether a password and a trusted-device
credential are stored, the password environment variable name, and whether
that variable is currently set. The command is fully offline: it never
resolves passwords and never contacts a NAS, so it is safe to run at any
time. JSON output additionally contains `client_cached` and `session_held`,
which describe the calling process only; in a fresh CLI process they are
always `false`, while a long-running MCP server reports its real cached
session state through the same model.

A `store_error` value means the OS credential store could not be probed for
that profile (for example, a locked keychain); other profiles still report
normally.

## Removal

```console
dsmctl auth logout --nas office
dsmctl auth logout --nas office --password
dsmctl auth logout --nas office --trusted-device
```

`auth logout` removes both stored credentials by default; the flags narrow
the scope. The output states, per credential, whether an entry existed. The
action is local-only and reversible with `dsmctl auth login`, which is why
it needs no plan/apply approval.

Three caveats apply and are printed by the command:

- Removal is local. DSM sessions held by other running dsmctl or MCP
  processes stay valid until those processes exit, and a running process
  keeps its in-memory password for automatic re-login until it exits.
- A set password environment variable (`password_env` or the
  `DSMCTL_PASSWORD_<NAME>` default) still enables non-interactive login
  after logout. `auth status` shows whether one is set.
- The profile name does not need to exist in the configuration, so
  credentials orphaned by an earlier `nas remove --keep-credentials` (or a
  removal performed by an older dsmctl release) can still be cleaned up.

`nas remove <name>` deletes the profile's stored credentials by default and
prints what it cleaned. Use `--keep-credentials` to keep them, for example
when the same profile name will be re-added later.

## Trusted-device rotation

```console
dsmctl auth rotate-device --nas office
```

Rotation deletes the stored trusted-device credential first — deleting first
is required, because a still-stored device lets DSM skip the OTP challenge
and no new device would be issued — and then authenticates with the stored
password or environment fallback. On a 2FA-protected account DSM prompts for
an OTP and returns a fresh device ID, which is saved to the OS credential
store.

The old device entry may remain listed server-side under DSM Personal >
Security; revoke it there. dsmctl deliberately does not call the DSM
trusted-device revocation API in this release. If login fails after the
deletion, nothing is left stored for the device; run `dsmctl auth login` to
recover. Other running processes that loaded the old device ID keep using it
until they exit.

## MCP

MCP exposes exactly one credential tool, the read-only `get_auth_status`,
which returns the same per-profile booleans and environment variable name as
`auth status --json`. It never returns secret values, never accepts a
password or OTP, and never contacts the NAS; its profile list reflects the
configuration loaded when the MCP server started. Credential removal and
rotation remain CLI-only. When authentication material is missing, an MCP
client should ask the user to run `dsmctl auth login --nas <name>` in a
terminal.
