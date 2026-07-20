# DSM error taxonomy and CLI exit codes

Every failure that originates from a DSM API is classified into a **stable,
closed** set of categories, so scripts and MCP clients can branch on the failure
class instead of matching opaque message text. The category strings and the CLI
exit codes below are part of the CLI contract — they do not change without a new
work item, and adding a category is likewise a contract change.

## Categories

`synology.Classify(err)` unwraps an error (through `fmt.Errorf("%w", …)`
wrapping) and returns one of:

| Category | Meaning |
| --- | --- |
| `auth` | The session or credentials are the problem — expired/invalidated session, wrong password, or a required/failed second factor. |
| `permission` | Authenticated, but the account lacks privilege (or is blocked by an access rule). |
| `not-found` | The target API, method, or resource does not exist. |
| `conflict` | The request conflicts with current state (duplicate, or a resource busy/in use). |
| `rate-limit` | DSM is throttling the caller. |
| `transient` | A temporary failure worth retrying (timeout, a 5xx from the web server, a reset). |
| `unsupported` | The operation or API version is not supported on this DSM. |
| `invalid-input` | A parameter was missing, malformed, or rejected. |
| `unknown` | No confident classification (the fallback). |

DSM API error codes map to categories as follows (unmapped codes fall back to
`unknown`): `101/114/120` → invalid-input; `102/103` → not-found; `104` →
unsupported; `105/108/402/407` → permission; `106/107/119` → auth (session);
`400/401/403/404/406` → auth (login / two-step). A `SessionExpiredError` and an
`OTPRequiredError` both classify as `auth`.

## CLI exit codes

`dsmctl` exits with a category-specific code so a script can react without
parsing stderr. The human-readable stderr line is prefixed with the category
when one is confidently classified (for example `Error (auth): …`).

| Exit code | Category |
| --- | --- |
| 0 | success |
| 1 | generic / unclassified (non-DSM) failure |
| 2 | invalid-input |
| 3 | auth |
| 4 | permission |
| 5 | not-found |
| 6 | conflict |
| 7 | rate-limit |
| 8 | transient |
| 9 | unsupported |

## Secret hygiene

A rendered DSM error never contains a SID, SynoToken, password, or OTP: the
`APIError` message is API/method/code only, and binary-transfer errors mask the
`_sid` / `SynoToken` query parameters (`url.URL.Redacted` masks only userinfo, so
the transport redacts those explicitly — see the FileStation transfer notes).

## Not yet surfaced

A machine-readable `category` field on every MCP tool error, and automatic
bounded retry of transient/rate-limit read-only calls, are a planned follow-on
(see [WI-060](../spec/work-items/WI-060-structured-dsm-errors.md)): the MCP field
needs a single error-middleware over all tool handlers, and retry needs the
request path to first classify HTTP-level timeouts/5xx/429 as transient.
