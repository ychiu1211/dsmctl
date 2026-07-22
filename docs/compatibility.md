# DSM compatibility architecture

DSM release numbers and WebAPI versions are different compatibility axes. `dsmctl` therefore selects an implementation per operation from a runtime target instead of constructing one client for an entire DSM generation.

## Release compatibility train

dsmctl's release version uses
`DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD`. The current `7.3.2-14` release
communicates DSM 7.3.2 as its latest certified feature train and build 13 as the
current dsmctl release in that train. The build number increases monotonically
for fixes and features that do not change the certified DSM train; moving the
train from 7.3.2 to a newer DSM release resets the build to 1 and still sorts
after every 7.3.2 build.

This product version is a communication and release-ordering convention. It
does not replace the runtime target below, imply that every DSM feature is
implemented, or cause requests intended for 7.3.2 to be sent blindly to older
systems. Older supported releases use the operations their advertised APIs and
verified release evidence permit. Exact DSM build/update certification is
recorded separately because `7.3.2-86009 Update 1`, for example, is more
specific than the `7.3.2` compatibility train.

## Target model

The compatibility target is assembled from:

1. APIs and min/max versions returned by `SYNO.API.Info`.
2. Capabilities derived from those APIs.
3. A DSM release/build parsed from a safe information operation.
4. Narrow transport or API quirks whose behavior cannot be handled universally.

API and capability matching is preferred. DSM release matching is reserved for a known behavior difference that cannot be identified more directly.

## Operation variants

An operation owns a compile-time list of variants:

```go
var operation = compatibility.Operation[Input, Result]{
    Name: "example.read",
    Variants: []compatibility.Variant[Input, Result]{
        commonV3,
        legacyV1,
    },
}
```

Every variant declares its backend name, API, exact WebAPI version, priority, matcher, and execution function. The router chooses the unique matching variant with the highest priority. Equal-priority matches are rejected as ambiguous instead of relying on registration order.

Before a non-bootstrap operation is selected, its client façade calls `prepareCompatibilityTargetLocked` with the operation's API names. This discovers the API catalog and obtains the DSM release through the System Info bootstrap operation, so a DSM-range override is eligible on the first call. The bootstrap operation must always retain at least one capability-based variant that does not require the DSM release to be known.

A release-specific override composes matchers and raises only that variant's priority:

```go
compatibility.Variant[Input, Result]{
    Name:     "dsm8-override",
    API:      "SYNO.Example",
    Version:  3,
    Priority: 100,
    Match: compatibility.All(
        compatibility.APIVersion("SYNO.Example", 3),
        compatibility.DSMVersionRange(
            compatibility.DSMVersion{Major: 8},
            compatibility.DSMVersion{Major: 9},
        ),
    ),
    Execute: executeDSM8,
}
```

Other operations continue using their common implementations on the same NAS.

## Package-scoped operations

Functionality provided by an installed package (Synology Drive, and later other
packages) versions with the **package release**, not the DSM release: the same
DSM build behaves differently under different installed versions of the same
package, sometimes without the advertised WebAPI version moving. Package-scoped
operations therefore add a third selection axis.

The compatibility target carries an installed-package catalog (stable package
id, parsed version, running flag) loaded from the verified Package Center
inventory operation. Matchers compose with the existing API and DSM matchers:

```go
compatibility.Variant[Input, Result]{
    Name:     "drive-connection-v1",
    API:      "SYNO.SynologyDrive.Connection",
    Version:  1,
    Priority: 10,
    Match: compatibility.All(
        compatibility.APIVersion("SYNO.SynologyDrive.Connection", 1),
        compatibility.PackageVersionRange(
            "SynologyDrive",
            compatibility.ParsePackageVersion("3.0"),
            compatibility.PackageVersion{}, // unbounded maximum
        ),
    ),
    Execute: executeList,
}
```

`PackageVersionRange(id, min, max)` matches an installed package version in
`[min, max)`; `PackageInstalled(id)` only requires presence. Both fail closed
when the catalog was never loaded, so missing evidence can never select a
backend. Package versions such as `4.0.3-27892` compare segment-wise with
missing segments as zero.

Two rules keep this axis honest:

1. **The catalog is refreshed before every package-scoped command.** The client
   façade re-reads the installed-package inventory before selecting a
   package-scoped operation, so a package updated mid-session cannot keep a
   stale variant selection, and the observed version is recorded in the
   selection reason and the report's `packages` list as evidence.
2. **Prefer advertised API versions when they move.** Like the DSM release
   rule, a package-version range is for a verified behavioral baseline or a
   verified per-package-version difference — not a substitute for API/version
   matching. A version-specific variant replaces only the affected operation
   and older, unverified package generations fail closed instead of receiving
   untested requests.

## Shared versus versioned code

Keep these concerns shared:

- TLS, HTTP, session cookies, SynoToken, retries, and error decoding.
- API discovery and requested-version validation.
- Canonical application models.
- Normalization helpers shared by compatible response shapes.

Create a new variant only for a changed API name, method, version, request schema, response schema, or semantic behavior. Small field-name differences should normally use shared normalization with variant-specific alias tables rather than duplicated clients.

## Fallback policy

The selector chooses a backend before executing it. It does not try every implementation after arbitrary failures.

Read operations may later implement a bounded fallback only for explicit unsupported-method or incompatible-schema signals. Authentication, authorization, TLS, network, and internal DSM errors must be returned directly.

## Live-verification evidence record

This table records the exact DSM build, and any relevant package version, each
module or operation group was live-verified against. It is **documentation of
observed history, not a runtime gate** — the compatibility selector never reads
it, and it must never be used to widen operation support beyond what advertised
APIs and verified release/package evidence already permit. It complements the
per-operation `capabilities` report, which reflects what the *connected* NAS
advertises at runtime.

| Module / operation group | DSM build verified | Relevant package version | Work item(s) |
| --- | --- | --- | --- |
| Package Center (inventory, PHP profile, guarded update) | DSM 7.3-81168 | — | WI-029 |
| Storage & SAN | DSM 7.3 | — | — |
| File Station (reads, sharing, transfer, thumbnail) | DSM 7.3 | — | WI-049 |
| Certificate (read) | DSM 7.3 | — | WI-065 |
| External Access (Synology Account, QuickConnect, DDNS, port forwarding) | DSM 7.3-81168 (DS3018xs) | QuickConnect API v3 | WI-041 |
| Download Station | DSM 7.3 | Download Station 4.1.2 | WI-043 |
| Synology Office (settings, system, fonts) | DSM 7.3.2-86009 Update 1 (DS923+) | Synology Office 3.7.2-22592 | WI-051, WI-052 |
| Synology Drive — team folders | DSM 7.3.2 | Synology Drive 4.0.3-27892 | WI-050 |
| Synology Drive — admin (nodes, connections, logs, activation) | DSM 7.3.2 | Synology Drive 4.0.3-27892 | WI-053–WI-057 |
| Login Portal (DSM access, application portals, reverse-proxy list) | DSM 7.3 | — | WI-070 |

### Adding a row

When an operation group is live-verified against a NAS, add or update its row
with:

1. The DSM build string from a safe information operation — prefer the most
   specific form available (`7.3.2-86009 Update 1`, `7.3-81168`), not just the
   `7.3` train.
2. The relevant package version (`stable id` plus parsed version, e.g.
   `Synology Office 3.7.2-22592`) for a package-scoped module, or `—` for a
   DSM-core module.
3. The work item(s) under which the verification was performed.

Record only what was actually observed live. A source-derived request shape that
was shipped without a live apply (for example a registration-style write the
safety policy forbids exercising) is noted in its work item's handoff, not added
here as verified.

Mutating operations must never switch variants after execution begins. Control Panel and SAN writes must select and validate one backend during planning, then apply through that same backend so a partial change cannot be followed by a second implementation.

## Tests

Every operation should have:

- Selector tests for API-version and DSM-release boundaries.
- Contract tests proving all variants produce the same canonical model.
- Sanitized response fixtures for important DSM releases.
- An unsupported-target test.
- Optional live smoke tests gated by environment variables.

`dsmctl nas capabilities` and the MCP `get_capabilities` tool expose the selected backend and match reason, making compatibility reports reproducible without exposing credentials.
