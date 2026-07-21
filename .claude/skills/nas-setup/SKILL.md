---
name: nas-setup
description: >-
  The map for taking a Synology NAS from bare hardware to a working, configured
  system with dsmctl, end to end. Use when the user wants the WHOLE journey —
  "completely set up / install and configure a NAS", "从头到尾裝好一台 NAS", "完整
  安裝跟設定", "把新機器弄到可以用", "bring up and configure a NAS", "set up a NAS
  from scratch" — or when they are unsure which step they are on. It lays out the
  four stages (install DSM → first administrator + wizard → storage → settings),
  the order and the decision points, and delegates the heavy steps to the focused
  skills: nas-install (OS install), nas-provision (first admin + wizard), and
  nas-storage-setup (storage volume). It also carries the hard-won operational
  knowledge — why the built-in admin must be disabled, the fresh-disk storage
  gotchas, how to set the server name — that a first-time operator would miss.
  Prefer running the stages SEPARATELY (storage is a deliberate decision), not as
  one long-blocking command.
---

# nas-setup — install and fully configure a Synology NAS end to end

Goal: take new/factory/reset hardware to a **working, configured NAS** — DSM
installed, a first administrator you own, the setup wizard finished, storage
built the way the operator wants, and the basic identity (server name) set. This
skill is the **map**; each heavy stage has its own focused skill. **Installing
DSM and creating storage both erase disks — destructive and irreversible.
Confirm the target with the user before any `--install` or storage apply.**

## Run it in STAGES, not one long command

The single biggest lesson: **do the stages separately.** Each stage is its own
`dsmctl` command, and the natural pause point is a working, inspectable NAS after
stage 2. **Storage (stage 3) is a decision, not a default** — RAID level, SHR,
one pool vs several, SSD cache, spares — and often needs a human/MIS discussion,
so it should not be crammed into the long-blocking install command, especially on
higher-end machines with many bays. A one-shot
`dsmctl install --admin-user <user> --create-volume` exists, but reserve it for
low-end/unattended boxes where the layout is a foregone conclusion.

## The journey at a glance

| Stage | What | Command / skill | Blocks? |
| --- | --- | --- | --- |
| 1 | Install DSM (the OS) | `dsmctl install` → **nas-install** skill | yes — download + reboot (minutes) |
| 2 | First administrator + finish wizard | `dsmctl provision` → **nas-provision** skill | no — fast; **→ working NAS** |
| *(pause)* | Inspect disks, decide the layout with the operator | `dsmctl --nas <n> storage inventory` / `capabilities` | — |
| 3 | Build storage | **nas-storage-setup** skill | yes — parity init runs in the background |
| 4 | Configure identity / settings | `dsmctl system set-name`, and more Control Panel modules | no |

## Stage 1 — Install DSM

Only if DSM is not installed (`not_install` / `sys_crash` / `sys_migrat`). Follow
the **nas-install** skill: detect state (read-only), then `--install` (online, or
offline by auto-downloading the matching `.pat`). It waits for the reboot and
stops at the https setup URL, e.g. `https://<ip>:5001`. If DSM is already
installed, skip to stage 2.

## Stage 2 — First administrator + finish the wizard

Follow the **nas-provision** skill:

```console
dsmctl provision <name> --url https://<ip>:5001 --admin-user <user> --insecure-skip-tls-verify
```

This creates the administrator (generated password → OS credential store), **and
finishes the DSM setup wizard**. Critically it **disables the built-in `admin`
account from the setup session** — that is what flips DSM's `admin_configured`
flag so the "Welcome to DSM" wizard stops appearing on login. `hide_welcome`
alone is not enough. After this you have a working NAS you can log into.

> Gotcha: DSM only lets the reserved `admin` be modified by the setup session
> itself; disabling it as the new admin fails with error 105. If a NAS
> provisioned before this was in place keeps showing the welcome wizard (its
> built-in admin is still enabled, likely empty-password), retrofit it:
> `dsmctl provision <name> --reset-builtin-admin`.

## Stage 3 — Build storage (a deliberate decision)

A fresh DSM has **no storage**, so shared folders and most packages don't work
until you build a volume. **Pause here** and decide the layout with the operator:

```console
dsmctl --nas <name> storage inventory       # disks present + their ids/state
dsmctl --nas <name> storage capabilities    # which RAID types this model supports
```

Then follow the **nas-storage-setup** skill — it asks how to build the volume
(default all-disk btrfs RAID5) and runs the two guarded plan/apply cycles (pool,
then volume). It carries the fresh-disk gotchas: a free disk's `sys_partition_normal`
status is normal and eligible; off-HCL lab drives need `allow_unsupported_disks`;
a fresh RAID's `background_optimizing` state is success and the volume is usable
immediately; DSM leaves the first volume's display name blank.

## Stage 4 — Configure identity and settings

Set the DSM **server name (hostname)** — the name shown in Control Panel and on
the network:

```console
dsmctl --nas <name> system set-name <server-name>        # confirms first
dsmctl --nas <name> system set-name <server-name> --yes  # unattended
```

It validates the hostname grammar, applies via `SYNO.Core.Network`, and verifies
by re-reading. (Note: `dsmctl system info` does NOT show the server name — DSM's
`SYNO.Core.System.info` omits it on 7.3.x; the network module is the authority.)
The provisioning `--device-name` flag sets the same value during stage 2, so this
is for renaming later or when you skipped it.

Beyond the server name, dsmctl exposes many other Control Panel / system modules
as guarded plan/apply operations — time & NTP (`dsmctl control-panel time`),
network reads (`dsmctl network`), certificates, firewall, login portal, file
services, notifications, and more. Run `dsmctl --help` to see the current surface;
configure only what the deployment needs.

## Security model (non-negotiable)

The administrator **username is the operator's choice** (never invented); the
**password is generated by dsmctl** with `crypto/rand`, used in-process, and
stored in the OS credential store. It is never printed, logged, returned, or put
in a plan, and **no MCP tool ever returns it**. A human retrieves it only at a
terminal:

```console
dsmctl auth password reveal --nas <name>       # prints (interactive terminal only)
dsmctl auth reveal-password --nas <name>       # copies to the clipboard
```

The model must not attempt the reveal — it is gated by isatty + a typed NAS-name
confirmation and refuses non-interactive/agent callers. When plaintext is needed,
tell the human to run the reveal in their own terminal.

## Verification

After all stages a NAS should: log in as `<user>` at `https://<ip>:5001` with no
welcome wizard, refuse the built-in `admin`, present a mounted volume at
`/volume1` (`dsmctl --nas <name> storage inventory`), and report the chosen server
name. Repo coverage lives under `go test ./internal/application/... ./internal/provision/...`;
the live-verified operational details are in the `dsm-initial-setup-provision`,
`dsm-first-time-storage-setup`, and `dsm-server-name-api` memories.
