# LAN device discovery

`dsmctl discover` finds Synology devices on the local network by speaking the
same UDP broadcast "findhost" protocol that Synology Assistant and DSM's
`findhostd` use. It is the one dsmctl feature that needs **no configured NAS
profile, credential, or DSM session**: it broadcasts a discovery query and
lists every Synology device that answers.

```console
dsmctl discover                 # scan for the default window, then list devices
dsmctl discover --timeout 15s   # or -t 15s; scan longer for a busy network
dsmctl discover --json          # emit the full discovery.Device model
dsmctl discover --cached        # print the saved results without scanning
dsmctl discover --clear         # discard the saved results
```

Discovery is read-only. It transmits only query packets and changes nothing on
any device.

The scan **re-broadcasts throughout the listen window and accumulates** the
answers, so it keeps filling in devices for the full `--timeout` (default 8s).
Press **Ctrl-C** to stop early and keep whatever has been found so far — devices
appear on standard error as they answer, so you can watch the list fill up and
stop once you see what you need.

Each run's results are **saved** and merged into a running set (see
[Saved results](#saved-results)), so a scan that happens to under-count does not
lose the devices an earlier scan found.

## Output

The text table has one row per device — hostname, model, OS version, IPv4
address(es), MAC, serial, and state:

```
HOSTNAME       MODEL     OS VERSION            IP ADDRESS                  MAC                SERIAL         STATE
office-nas     DS923+    7.3.2-86009 Update 1  192.0.2.82                  00:00:5E:00:53:04  TESTSERIAL0003 ready
lab-rack       DS3622xs+ 8.0-120105            192.0.2.112, 192.0.2.14     00:00:5E:00:53:03  TESTSERIAL0004 ready
```

`--json` emits the full `discovery.Device` model, including the netmask,
gateway, DNS, build number, and RAID-support flag that the table omits.

A device that answers on more than one interface (a multi-homed or High
Availability unit) is reported **once**, keyed by serial number, with every
observed address collected in `ipv4_addresses`.

### State

`state` is the device's own quick-config status, mapped to a stable label —
most commonly `ready` (installed and running). Other values include
`not_installed` (a fresh or reset unit awaiting installation), `migratable`
(disks moved from another unit), `recoverable`, `installing`, `booting`, and
`memory_testing`. When a device sends no status, the state is inferred from the
response type.

## How it works

- dsmctl binds UDP port **9999** and broadcasts a findhost query — an 8-byte
  `12 34 56 78 'S' 'Y' 'N' 'O'` header followed by the packet-type and
  protocol-version fields `findhostd` requires — to the limited broadcast
  address and to every up interface's directed broadcast, so multi-homed hosts
  reach every attached subnet.
- The query is re-sent in a few quick initial bursts and then once a second for
  the rest of the window. Every device answers every broadcast, so repeated
  rounds recover devices a single round missed and catch devices that power on
  partway through the sweep.
- Devices answer by **broadcasting** their reply to port 9999. dsmctl parses
  each response, decoding the protocol's mixed endianness (IP-address fields are
  network byte order; other integers are little endian) and deduplicating
  answers by device.
- Discovery is unencrypted: dsmctl sends no public key, so `findhostd` replies
  in plaintext. Encrypted responses from other clients are ignored.

Only devices in the **same broadcast domain** as the host running dsmctl can
answer; broadcast queries do not cross routers.

## Saved results

Every scan writes its devices to `discovered.json`, a sibling of the
configuration file (so it follows the same per-user config directory). Devices
are merged into a **running union** across runs: each device keeps its
`first_seen` time and has its `last_seen` time refreshed whenever it answers
again. A scan that finds nothing leaves the saved set untouched.

- `dsmctl discover --cached` prints the saved set — with a `LAST SEEN` column —
  without scanning.
- `dsmctl discover --clear` discards the saved set.

The saved set is the reliable record when an individual scan under-counts (see
below): even if one run answers with only a handful of devices, the union from
previous runs is preserved and available with `--cached`.

Persistence lives in the shared application layer (`Service.DiscoverDevices`,
`CachedDiscoveries`, `ClearDiscoveries`), so the CLI and the local MCP server are
just two entry points onto the **same** saved file: a scan from either updates
the union, and either can read it back. An MCP scan therefore builds the same
record a CLI scan does.

## Sharing UDP 9999 with Synology Assistant

`findhostd` broadcasts its replies to port 9999, so dsmctl must bind 9999 to
receive them. **Synology Assistant, when running, holds UDP 9999 continuously.**
On Windows, a broadcast datagram arriving at a port that two processes share is
delivered to only **one** of them, so a dsmctl scan and Synology Assistant end up
splitting the replies. A scan that loses the split answers with far fewer devices
than are really present.

In practice the first scan after the port has been idle for a few seconds
usually wins and sees everything; scans run in rapid succession are the ones that
can under-count. dsmctl mitigates this three ways:

- it re-broadcasts and accumulates across the whole window, recovering devices
  round by round;
- it saves the running union, so an under-counting scan does not lose ground; and
- when a scan answers with noticeably fewer devices than the saved set, it prints
  a note pointing at `dsmctl discover --cached` and suggesting you wait a few
  seconds and rerun.

Closing Synology Assistant removes the contention entirely.

## Scope and limitations

- Discovery is intentionally outside dsmctl's per-NAS DSM WebAPI contract: it
  uses no session, no WebAPI call, and no compatibility routing, because it is
  a connectionless, unauthenticated LAN probe. It therefore does not appear in
  the per-NAS capability report.
- It never sends network-setting, quick-config, or install packets — it only
  reads.

## MCP

The local MCP server exposes discovery as the read-only tool
`discover_lan_devices`. It shares the CLI's discovery code end to end — the same
re-broadcasting sweep and the same saved set:

- `timeout_seconds` (optional, default 8, max 60) — like the CLI's `--timeout`, a
  larger window returns a more complete set under contention.
- `cached` (optional bool) — like `--cached`, return the saved cross-run set
  without scanning.
- A scan merges into the same `discovered.json` the CLI uses, and every response
  reports `saved_total`, the size of the saved union after the scan — larger than
  the returned count when the scan under-counted.

Clearing the saved set (the CLI's `--clear`) is a local maintenance action and is
not exposed over MCP; because the file is shared, `dsmctl discover --clear`
clears it for both.

The managed gateway exposes the same application operation to its authenticated
local administrator while the Add NAS wizard is open. Remote MCP bearer tokens
may also invoke the tool only when they hold the separate `lan.discover` scope;
the NAS allowlist does not grant discovery implicitly because a scan can reveal
devices outside that allowlist.
