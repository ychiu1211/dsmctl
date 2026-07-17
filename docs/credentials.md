# Credential lifecycle

dsmctl signs in to a NAS through the DSM web login: `dsmctl auth login` opens
the NAS's own sign-in page in the browser, where the password (and any
two-factor, passkey, or approve-sign-in step) stays. What dsmctl stores, per
NAS profile, is the resulting session: the live SID and SynoToken, the
renewal keys that will allow refreshing the session without a browser, the
DSM device ID, and non-secret metadata such as the account name and issue
time. Passwords are never stored by this release. This guide covers
inspecting and removing stored sessions. No command in this guide ever
prints a secret value.

## Where sessions are stored, and why there is no extra encryption

Sessions live in the operating system's credential store: Windows Credential
Manager, macOS Keychain, or the Linux D-Bus Secret Service
(gnome-keyring/KWallet). The store provides at-rest encryption tied to the
OS login, isolates secrets from other OS users, and keeps them out of
config files, dotfile syncs, and backups. dsmctl deliberately adds no
encryption layer of its own: a wrapping key would have to live on the same
machine with the same protections, so it would add obfuscation, not
security. Like every credential helper, the store does not protect against
software already running as the same OS user.

Platform limits worth knowing:

- On a headless Linux host (no D-Bus Secret Service — containers, plain SSH
  sessions, WSL without a keyring daemon) the store is unavailable. dsmctl
  reports the store error and stores nothing rather than writing secrets to
  disk.
- On Linux desktops with auto-login the keyring may still be locked, and the
  Secret Service prompts or fails until it is unlocked.
- Windows caps one Credential Manager entry at 2560 bytes. A stored session
  is a few hundred bytes, so there is ample headroom, but new
  `SessionCredential` fields must keep this limit in mind.

## Status

```console
dsmctl auth status
dsmctl auth status --nas office --json
```

The table reports, per profile, whether a web-login session is stored,
whether it carries renewal keys, and the account it belongs to. The command
is fully offline: it never reveals secrets and never contacts a NAS, so a
reported session may still have expired server-side. JSON output adds the
legacy password/trusted-device presence fields, the password environment
variable name and set state (the automation fallback), and `client_cached` /
`session_held`, which describe the calling process only; in a fresh CLI
process they are always `false`, while a long-running MCP server reports its
real cached session state through the same model.

A `store_error` value means the OS credential store could not be probed for
that profile (for example, a locked keychain); other profiles still report
normally.

## Sign-out

```console
dsmctl auth logout --nas office
```

`auth logout` first asks DSM to revoke the session, then deletes the stored
copy (including its renewal keys) from the OS credential store. `nas remove`
performs the same sign-out for the profile it deletes (unless
`--keep-credentials` keeps the session usable). Revocation is best-effort,
waits only a few seconds, and its failure never blocks the local removal:

- When the NAS is unreachable, the command warns, removes the local copy
  anyway, and the DSM session lapses on its own server-side expiry.
- The profile name does not need to exist in the configuration, so a session
  orphaned by an earlier `nas remove --keep-credentials` can still be cleaned
  up — but without a configured URL the NAS cannot be contacted, so the
  removal is local-only and the command says so.
- A successful revocation invalidates that session everywhere, including in
  other running dsmctl or MCP processes that loaded the same stored session;
  their next call fails with an instruction to sign in again.

Signing in again is always `dsmctl auth login`. Because sign-out is
reversible this way, it needs no plan/apply approval.

`nas remove <name>` signs out of and deletes the profile's stored session by
default and also best-effort cleans any password or trusted-device entry left
behind by an older dsmctl release. Use `--keep-credentials` to keep them, for
example when the same profile name will be re-added later.

## MCP

The MCP server reuses the same stored web-login sessions as the CLI, and the
password environment variable (`DSMCTL_PASSWORD_<NAME>` or the profile's
`password_env`) remains a non-interactive fallback for automation. When the
session a long-running MCP server started with expires, the server recovers
on the next call without a restart: it re-reads the store (picking up a
fresh `dsmctl auth login`) or renews the session with its stored renewal
keys. MCP
exposes exactly one credential tool, the read-only `get_auth_status`, which
returns the same per-profile fields as `auth status --json`. It never
returns secret values, never accepts a password or OTP, and never contacts
the NAS; its profile list reflects the configuration loaded when the MCP
server started. Sign-in and sign-out remain CLI-only: when authentication
material is missing, an MCP client should ask the user to run
`dsmctl auth login --nas <name>` in a terminal.
