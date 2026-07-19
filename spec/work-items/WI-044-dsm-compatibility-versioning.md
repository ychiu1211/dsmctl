---
id: WI-044
title: Align release versions with the certified DSM compatibility train
status: done
priority: P1
owner: ""
depends_on: []
parallel_group: E
touches:
  - internal/buildinfo
  - cmd/dsmctl-mcp
  - cmd/dsmctl-gateway
  - deploy/container/Dockerfile
  - .github/workflows/ci.yml
  - .github/workflows/gateway-release.yml
  - deploy/synology/build-spk.sh
  - docs/compatibility.md
  - docs/synology-package.md
  - README.md
---

# WI-044 - Align release versions with the certified DSM compatibility train

## Outcome

All dsmctl front ends and release artifacts identify the current release as
`7.3.2-1`: `7.3.2` is the certified DSM compatibility train and `1` is the
dsmctl build revision within that train.

## Scope

- Define and document the `DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD` policy.
- Give source builds a shared current version and expose it from CLI, stdio MCP,
  and gateway binaries.
- Make release tags, manual release inputs, container metadata, and SPK artifact
  names consume the exact full version rather than appending an unrelated CI
  run number.
- Update release examples to the current `7.3.2-1` train.

## Non-goals

- Automatic download, staging, activation, or rollback.
- Replacing operation-scoped DSM capability selection with a global version
  gate.
- Claiming that every operation works on every DSM release below 7.3.2.

## Design constraints

- The compatibility train communicates the latest certified DSM feature
  baseline; individual operations remain selected from advertised APIs and
  narrowly scoped release evidence.
- The final build component is a positive integer and increases monotonically
  within one compatibility train.
- All three front ends built from one revision report the same version.

## Acceptance criteria

- [x] Default builds report `7.3.2-1` from CLI, stdio MCP, and gateway version
      entry points.
- [x] The release workflow accepts only `X.Y.Z-N`, requires its value to match
      the source version, and embeds it unchanged in image/SPK metadata.
- [x] Documentation distinguishes the product compatibility train from exact
      verified DSM builds and operation-level support.
- [x] Unit tests, `go test ./...`, `go vet ./...`, and all three builds pass.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- Build and run `--version` for `dsmctl`, `dsmctl-mcp`, and `dsmctl-gateway`.
- No live DSM call or mutation is required.

## Coordination

The release workflow and Synology build script overlap the in-progress WI-017
distribution files. This item changes only release-version parsing and examples;
it does not change the image, SPK lifecycle, DSM support matrix, or hardware
certification state.

## Handoff

- Completed source and release version policy at `7.3.2-1`, including strict
  parsing/ordering, shared binary reporting, container/SPK consistency checks,
  release tags, CI checks, and user documentation.
- Verified `go test ./... -count=1`, `go vet ./...`, all three Go builds,
  `git diff --check`, and `bash -n deploy/synology/build-spk.sh`.
- Verified the CLI, stdio MCP, and gateway report `7.3.2-1`; a local
  `linux/amd64` container build reported the same binary version and OCI image
  label. The uniquely tagged test image was removed after verification.
- No DSM connection, live DSM call, or mutation was performed.
