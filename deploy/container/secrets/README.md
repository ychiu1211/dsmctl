# Gateway secrets

Create these owner-readable files before first start:

- `master.key`: exactly 32 random binary bytes; never place it in `/data` or a
  normal database backup.

There is no administrator bootstrap or platform-identity secret. Open the
first-run page to create the local administrator account before exposing the
Gateway beyond its trusted deployment network.

Managed MCP tokens are created in the administration page and stored as
digests in `gateway.db`; they are not mounted as container secrets. The
`--dev-read-only-token-file` flag is only for static WI-014 developer mode.

An optional `dsm-passwords.env` may retain the environment-password fallback
for narrowly scoped automation accounts, but the admin web-login/password+OTP
enrollment and encrypted vault are the normal managed path. Files in this
directory are mounted read-only and ignored by Git.
