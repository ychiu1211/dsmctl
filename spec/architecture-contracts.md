# Architecture contracts

These constraints apply to every work item unless an approved spec explicitly
replaces one of them.

## Dependency direction

```text
CLI ---------+
             +--> application --> runtime/session manager --> Synology facade
MCP server --+                                               |
                                                             +--> operation variant
                                                                     |
                                                                     +--> WebAPI executor
```

- CLI and MCP are thin adapters. They do not construct DSM requests.
- MCP never shells out to the CLI.
- The Synology facade does not know about Cobra, MCP, config files, or prompts.
- Domain models do not expose DSM request field names when a stable semantic
  name is available.

## Compatibility

- Selection is per operation, not per monolithic DSM client.
- Prefer advertised API/version capabilities. Use a DSM release range only for
  a verified behavior difference.
- A version-specific variant replaces only the affected operation.
- Every new operation appears in capability reports with a stable operation
  name, selected backend, API, and version.
- Decoders normalize DSM responses and return errors for malformed shapes; they
  must not silently return an empty successful state.

## Mutation safety

- Do not expose a generic raw WebAPI mutation command or MCP tool.
- Mutations use plan/apply unless the action is local-only and reversible.
- Plans include the NAS profile, canonical intent, stable resource identifier,
  observed-state fingerprint, risk, summary, and approval hash.
- Apply revalidates the plan, rereads relevant state, rejects stale state,
  performs a typed operation, and verifies the postcondition.
- Partial resources must state ownership semantics explicitly: full desired
  state or patch-only. Unspecified fields must never be silently reset.
- Destructive and privilege-reducing changes are marked high risk.

## Secrets and identity

- Passwords, OTPs, encryption keys, recovery material, SIDs, and SynoTokens do
  not enter display models, plans, logs, or MCP tool arguments.
- Secrets are referenced and resolved only at apply time.
- Built-in or current-session principals require an explicit protection policy.
- Fan-out inventory is opt-in and supports focused principal/target filters.

## Product surfaces

Each completed management slice normally contains:

1. Stable domain state and change intent.
2. One or more independently selectable operation variants.
3. Capability reporting.
4. Application validation and plan/apply policy.
5. Thin CLI commands.
6. Thin MCP tools using the same application methods.
7. Unit fixtures/request-capture tests and proportionate integration tests.
8. User documentation and an updated work-item status.

## Remote gateway and deployment boundary

The portable gateway extends the dependency graph without changing DSM
operation ownership:

```text
Generic Linux adapter --+
                        +--> gateway transport/policy --> application --> runtime --> Synology facade
Synology SPK adapter ---+
```

- Deployment adapters own process lifecycle, ports, reverse-proxy/TLS wiring,
  and persistent mounts. Gateway administrator authentication stays inside the
  platform-neutral core and is identical on Linux and Synology. Deployment
  adapters never construct DSM
  requests, resolve DSM operation variants, or bypass the application layer.
- The core gateway container is platform-neutral. It must not depend on DSM
  paths, commands, package environment variables, Container Manager control,
  the Docker socket, or a desktop keyring.
- Authentication occurs before remote MCP session/tool execution. The verified
  caller and target/scope policy reach an enforceable gateway application
  boundary; tool annotations and client-supplied identity are never authority.
- Remote authorization is additive to the existing plan/apply checks. A plan
  hash proves artifact integrity, not human approval. High-risk remote apply
  requires a separate short-lived, single-use approval.
- Persistent ciphertext and its master key use separate mounts. Missing or
  invalid key material fails closed and must not overwrite stored data.
- Fan-out mutation is prohibited. Read-only fleet fan-out is opt-in, bounded,
  target-filtered, and returns per-target success or error without hiding
  partial results.

The complete platform and packaging decisions are in
[`gateway-deployment.md`](gateway-deployment.md).
