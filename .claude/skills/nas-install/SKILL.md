---
name: nas-install
description: >-
  Bring up a factory-fresh, reset, or crashed Synology NAS with dsmctl: detect its
  install state, install DSM (online, or offline by auto-downloading the matching
  .pat from Synology when the device has no internet), create the first
  administrator (password in the OS credential store), and finish the DSM setup
  wizard (disabling the built-in admin so the welcome wizard stops) — leaving a
  working, manageable NAS. Prefer to run it in STAGES (install, then provision,
  then storage) because the storage layout is a deliberate human/MIS decision;
  building storage is the separate nas-storage-setup skill. A one-shot
  `dsmctl install --admin-user <user> --create-volume` exists for
  simple/low-end/unattended boxes. Use when asked to "install DSM", "set up a new
  NAS", "complete the installation", "reinstall a broken NAS", "裝好一台全新/reset
  的 NAS", "線上安裝 DSM", "完成安裝", or when a discovered device reports state
  not_install / sys_crash / sys_migrat.
---

# Bring up a fresh Synology NAS (install DSM → first admin → storage)

Goal: take a NAS that has no usable DSM to a working, manageable DSM (installed,
first administrator created, setup wizard finished, built-in `admin` disabled),
and then — as a **separate, deliberate step** — build its storage. Everything is
a `dsmctl` invocation; this skill is the order of operations and the decision
points. **Installing DSM and creating a volume both erase the device's disks —
destructive and irreversible. Confirm the target with the user before
`--install` / `--create-volume`.**

## Two ways to run it — prefer STAGED

The steps are separate commands, so run them as distinct stages or chain them.
**Storage is a decision, not a default:** how to lay out the disks (RAID level,
SHR, one pool vs several, SSD cache, spare) usually needs a human/MIS call, and
it should not be rushed inside a long-running install. So keep it its own stage —
especially on higher-end machines with many bays.

**Staged (recommended).** Bring the box up to a working, inspectable NAS first,
then decide storage separately when you and the operator are ready:

```console
# 1. Install DSM. This blocks through download + reboot (minutes); run it in the
#    background and monitor if the harness has a timeout. It stops at "DSM up".
dsmctl install --url http://<ip>:5000 --install --yes

# 2. Create the first admin + finish the wizard (fast). Now it is a working NAS
#    you can log into and inspect.
dsmctl provision <name> --url https://<ip>:5001 --admin-user <user> --insecure-skip-tls-verify

# 3. Read the disks and DECIDE the layout with the operator, THEN build storage
#    (follow the nas-storage-setup skill — it asks how to build the volume):
dsmctl --nas <name> storage inventory       # disks present
dsmctl --nas <name> storage capabilities    # which RAID types this model supports
```

(You can fold step 2 into step 1 with `dsmctl install --admin-user <user>`; the
box still stops before storage.)

**One-shot (simple / low-end / unattended only).** When the layout is a foregone
conclusion — e.g. a small box you always build as one all-disk btrfs RAID5 — you
*can* chain everything. Note this single command blocks through install, reboot,
first-setup, AND volume creation, so use it only when no storage discussion is
needed:

```console
dsmctl install --url http://<ip>:5000 --install --yes \
    --admin-user <user> --create-volume
# --raid raid5 / --filesystem btrfs (defaults); --allow-unsupported-disks for lab drives
```

The sections below are each step in detail, and the decision points behind each flag.

## 1. Find the device and its Web Assistant URL

The install endpoints live on the Web Assistant port (5000 http / 5001 https),
not the DSM API. Get the address from the user, or discover it:

```
dsmctl discover                 # lists LAN Synology devices with their state
```

A device whose state is `not_install`, `sys_crash`, or `sys_migrat` needs
installing. `dsmctl discover` cannot tell an installed-but-no-admin box from a
configured one, but the install detector below is authoritative.

## 2. Detect state (read-only — always do this first)

```
dsmctl install --url http://<ip>:5000
```

This prints model, serial, disk count, the state, and whether an install is
available **online** (the device can reach Synology) or must be done **offline**
(the host downloads the `.pat`). It changes nothing. States:

- `not_install` — no DSM; fresh/reset hardware → install.
- `sys_crash` — DSM installed but broken → reinstall.
- `sys_migrat` — disks moved from another NAS → migrate (not yet automated).

If it prints "not reachable online", the command will fall back to downloading
the matching image; the exact `.pat` URL it would use is shown.

## 3. Install DSM (destructive — needs the user's OK)

Same command with `--install`. It refuses unless you retype the device serial,
or pass `--yes` for automation. It auto-chooses:

- **Online** when the device reports internet access (fastest; the device
  downloads DSM itself).
- **Offline** when the device has no internet: the *host* downloads the DSM
  image that matches the device's own flash build from Synology, then uploads it.
  Use `--pat <file>` to supply a local image instead.

```
dsmctl install --url http://<ip>:5000 --install            # prompts for serial
dsmctl install --url http://<ip>:5000 --install --yes      # automation
dsmctl install --url http://<ip>:5000 --install --pat /path/DSM_<model>_<build>.pat
```

The command triggers the install, polls progress, and waits for the NAS to
reboot and DSM to come up (detected by `SYNO.API.Info` answering at the https
setup URL). This takes several minutes (plus download/upload time offline); run
it in the background and monitor if the harness has a timeout. When it finishes
it prints the setup URL, e.g. `https://<ip>:5001`.

## 4. Create the first administrator (and finish the wizard)

Adding `--admin-user` to the install command does this automatically right after
DSM comes up. To do it as a separate step (or against an already-installed NAS),
use `dsmctl provision`:

```console
dsmctl provision <profile-name> --url https://<ip>:5001 --admin-user <user> --insecure-skip-tls-verify
```

This creates the admin, **disables the built-in `admin` account from the setup
session** (which is what makes DSM stop showing the "Welcome to DSM" wizard —
disabling admin flips DSM's `admin_configured` flag), finishes the wizard, and
hardens. `--insecure-skip-tls-verify` accepts the device's fresh self-signed
certificate (a lab convenience; interactively you would pin it instead). The
generated password lands in the OS credential store under the profile name and is
never printed; retrieve it later, at a terminal, with
`dsmctl auth password reveal --nas <profile-name>`.

(If a NAS provisioned before this fix keeps showing the welcome wizard, its
built-in admin is still enabled — retrofit with
`dsmctl provision <profile-name> --reset-builtin-admin`.)

## 5. Create the first storage volume (makes the NAS usable)

Adding `--create-volume` to the install command builds one storage volume across
ALL disks after provisioning (default all-disk btrfs RAID5). A fresh DSM has NO
storage, so shared folders and most packages do not work until this runs.

```console
dsmctl install ... --admin-user <user> --create-volume [--raid raid5] [--filesystem btrfs] [--allow-unsupported-disks]
```

To build storage as a separate step (or on an already-installed NAS), follow the
**nas-storage-setup** skill — it asks the user for the RAID/filesystem layout and
runs the two guarded plan/apply cycles (pool, then volume) with the fresh-disk and
lab-drive handling.

## Notes and limits

- **Match the image to the model/build.** Offline install auto-derives the
  Synology URL from the device's reported model + flash build
  (`.../release/<ver>/<build>/DSM_<model>_<build>.pat`); an explicit `--pat`
  must be the right platform or the device rejects it.
- **Offline still needs internet on the host**, just not on the device.
- **Not yet automated:** `sys_migrat` (migration), and surfacing install through
  the gateway/MCP (it is CLI-only today).
- Protocol reference: the `/webman/*.cgi` install API is documented in the
  `dsm-web-assistant-install-api` memory and `internal/provision/install.go`.
