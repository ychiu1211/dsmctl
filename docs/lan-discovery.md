# LAN device discovery

`dsmctl discover` finds Synology devices on the local network by speaking the
same UDP broadcast "findhost" protocol that Synology Assistant and DSM's
`findhostd` use. It is the one dsmctl feature that needs **no configured NAS
profile, credential, or DSM session**: it broadcasts a discovery query and
lists every Synology device that answers.

```console
dsmctl discover
dsmctl discover --timeout 5s
dsmctl discover --json
```

Discovery is read-only. It transmits only query packets and changes nothing on
any device.

## Output

The text table has one row per device — hostname, model, OS version, IPv4
address(es), MAC, serial, and state:

```
HOSTNAME       MODEL     OS VERSION            IP ADDRESS                  MAC                SERIAL         STATE
office-nas     DS923+    7.3.2-86009 Update 1  10.17.32.82                 90:09:d0:41:b7:ed  2340TQRMS02XQ  ready
lab-rack       DS3622xs+ 8.0-120105            10.17.34.112, 10.17.37.14   00:11:32:de:75:80  2090SQRZADD2F  ready
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
- Devices answer on port 9999. dsmctl parses each response, decoding the
  protocol's mixed endianness (IP-address fields are network byte order; other
  integers are little endian) and deduplicating answers by device.
- Discovery is unencrypted: dsmctl sends no public key, so `findhostd` replies
  in plaintext. Encrypted responses from other clients are ignored.

Only devices in the **same broadcast domain** as the host running dsmctl can
answer; broadcast queries do not cross routers.

## Scope and limitations

- Discovery is intentionally outside dsmctl's per-NAS DSM WebAPI contract: it
  uses no session, no WebAPI call, and no compatibility routing, because it is
  a connectionless, unauthenticated LAN probe. It therefore does not appear in
  the per-NAS capability report.
- It never sends network-setting, quick-config, or install packets — it only
  reads.
- If a sweep started immediately after another returns no devices, the
  well-known port was still being released; re-run it.

## MCP

The local MCP server exposes discovery as the read-only tool
`discover_lan_devices` (optional `timeout_seconds`, default 3, max 60). It is
**not** exposed on the remote gateway surface: a remote caller must not be able
to trigger a broadcast scan of the gateway host's local network.
