# Testing policy

`dsmctl` has two tiers of tests: a default gate that runs everywhere with no NAS
reachable, and opt-in live tests that talk to a real Synology NAS and are never
run in CI.

## Default gate

```console
go build ./...
go vet ./...
go test ./...
```

The default `go test ./...` is unit tests plus request-capture tests: they use
`httptest` servers and recorded WebAPI shapes, never a real NAS. This is the
gate on every push and pull request, and it must pass with **no NAS reachable
and no live environment variables set**. CI runs this gate on an OS matrix of
`ubuntu-latest` and `windows-latest` (see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)).
The Docker cross-compile, gateway image build, and hardened-container smoke test
depend on Docker and `linux/amd64`, so they run only on the Linux leg.

## Opt-in live tests

The `integration/` package holds live tests. Every one begins with a
`t.Skip` unless the environment tells it to run, so `go test ./...` skips them
by default. The environment-variable contract is:

| Variable | Purpose |
| --- | --- |
| `DSMCTL_LIVE_NAS` | NAS profile name the live test targets |
| `DSMCTL_LIVE_CONFIG` | Optional path to the dsmctl config holding that profile |
| `DSMCTL_MCP_BINARY` | Path to a built `dsmctl-mcp` the test drives over stdio |
| `DSMCTL_LIVE_MUTATIONS=1` | Authorizes the account/share live mutation test |
| `DSMCTL_LIVE_SAN_MUTATIONS=1` | Authorizes the one disposable-LUN SAN test |

Read-only live smokes (for example `TestMCPGetSystemInfoLive`) need only
`DSMCTL_MCP_BINARY` and `DSMCTL_LIVE_NAS`. Mutating live tests additionally
require their explicit `*_MUTATIONS=1` authorization flag.

CI **guards** these: a workflow step fails the run if any of the five variables
is set, so a live or destructive mutation can never execute in CI, and a
follow-up step runs `go test ./integration -run Live -v` to confirm the
NAS-dependent tests report skipped.

## Live-mutation safety rules

Mirrors the authoritative safety default in [`AGENTS.md`](../AGENTS.md) and
`spec/README.md` — this section restates it, it does not weaken it:

- **Never** run storage-pool, volume, SAN target/mapping, encrypted-share, WORM,
  network, firewall, or other disruptive live mutations without explicit
  authorization for that exact test.
- Live tests may only touch **unique `dsmctl-e2e-*` resources**, never
  pre-existing user data.
- Cleanup must be **stable-ID-verified**: a resource is deleted only after its
  own stable DSM ID is confirmed, so a name collision can never delete the wrong
  object. Disposable LUN create/delete is authorized only for a unique
  `dsmctl-e2e-lun-*` LUN that is never mapped and is removed only after its
  stable LUN ID is verified.
- A source-derived write that the safety policy forbids exercising live (for
  example a registration-style external write) ships with unit tests that assert
  the request wire shape and is documented as not-live-applied in its work
  item's handoff — it is never forced through a live apply to "prove" it.

## DSM compatibility evidence

Which DSM build and package version each module was live-verified against is
recorded in [`compatibility.md`](compatibility.md#live-verification-evidence-record),
not only in agent memory. Add a row there whenever an operation group is
live-verified against a NAS.
