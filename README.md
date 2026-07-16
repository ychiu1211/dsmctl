# dsmctl

`dsmctl` is a Go client for managing one or more Synology DSM systems. One repository produces two front ends backed by the same typed DSM WebAPI client:

- `dsmctl`: a command-line interface for administrators.
- `dsmctl-mcp`: a stdio MCP server for AI clients.

The first milestone intentionally implements one complete vertical slice: configure multiple NAS profiles, authenticate with password and DSM two-factor authentication, maintain independent sessions, and read basic system information.

## Architecture

```text
CLI ---------+
             +--> application --> runtime/session manager --> Synology WebAPI client --> DSM
MCP server --+                           |                         |
                                         +--> OS credential store +--> API discovery
```

CLI and MCP never construct raw DSM requests. Both use the same application, session, credential, and WebAPI packages, so changes to DSM behavior are made once.

## Build

Go 1.25 or newer is required.

```console
go test ./...
go build -o bin/dsmctl ./cmd/dsmctl
go build -o bin/dsmctl-mcp ./cmd/dsmctl-mcp
```

On Windows, use `bin/dsmctl.exe` and `bin/dsmctl-mcp.exe`.

## Quick start

Add a NAS profile:

```console
dsmctl nas add office --url https://nas.example.com:5001 --username automation --default
```

Authenticate interactively:

```console
dsmctl auth login --nas office
```

The password and DSM trusted-device credential are stored in the operating system's credential store, not in `config.json`. Password and OTP prompts are hidden. If DSM requests an OTP, `dsmctl` exchanges it for a trusted-device ID so later CLI and MCP processes can authenticate without transporting OTP values through an AI client.

Read system information:

```console
dsmctl system info
dsmctl system info --nas office --json
```

Manage more than one NAS:

```console
dsmctl nas add lab --url https://192.168.10.20:5001 --username automation
dsmctl nas list
dsmctl auth login --nas lab
dsmctl system info --nas lab
dsmctl nas use lab
```

The default configuration path is the platform user-config directory, such as `%AppData%\dsmctl\config.json` on Windows. Override it with `--config` or `DSMCTL_CONFIG`.

Example configuration (no secret values):

```json
{
  "default_nas": "office",
  "nas": {
    "office": {
      "url": "https://nas.example.com:5001",
      "username": "automation",
      "password_env": "DSMCTL_PASSWORD_OFFICE",
      "timeout_seconds": 30
    }
  }
}
```

`password_env` remains available as a non-interactive password fallback:

```powershell
$env:DSMCTL_PASSWORD_OFFICE = "your-password"
```

For a 2FA-protected account, run `dsmctl auth login` once on the same host so DSM's trusted-device credential can be placed in the OS credential store.

Use `--insecure-skip-tls-verify` only for a test NAS with a certificate that cannot be trusted. TLS verification is enabled by default.

## MCP server

Run the stdio server:

```console
dsmctl-mcp --config C:\path\to\config.json
```

Available tools:

- `list_nas`: list safe profile metadata; secrets are never returned.
- `get_system_info`: authenticate to a selected profile and return normalized DSM system information.

MCP intentionally has no tool that accepts a password or OTP. If interactive authentication is required, it returns an actionable error asking the user to run `dsmctl auth login --nas <name>` first.

An opt-in live smoke test verifies the real stdio process boundary:

```powershell
$env:DSMCTL_MCP_BINARY = (Resolve-Path .\bin\dsmctl-mcp.exe).Path
$env:DSMCTL_LIVE_NAS = "office"
go test ./integration -run TestMCPGetSystemInfoLive -v
```

## Security model

- Passwords, OTP values, DSM device IDs, SIDs, and SynoTokens are never stored in the config file or returned by CLI/MCP display models.
- Login parameters use an HTTPS POST form, not URL query parameters.
- OTP values are short-lived and never persisted.
- Passwords and trusted-device IDs use Windows Credential Manager, macOS Keychain, or Linux Secret Service.
- Every NAS profile owns a separate session and trusted-device credential.
- DSM session errors 106 and 119 trigger one automatic re-login and retry.

## Adding commands

A management feature normally adds:

1. A typed operation under `internal/synology`.
2. A use case and policy under `internal/application`.
3. Thin CLI and MCP adapters.

Raw generic DSM calls are not exposed as MCP tools. Mutating operations will use guarded plan/apply semantics as the project expands into Control Panel and SAN management.
