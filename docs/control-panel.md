# Control Panel modules

Control Panel support is organized as small typed modules. Each module owns
its state, capability names, DSM API variants, CLI subtree, and MCP tools. This
keeps a DSM-version change in one setting area from turning the shared wrapper
into an untyped `set key=value` API.

## Time and NTP

The first module is read-only and returns the configured time zone, DSM date
and time display formats, synchronization mode, and ordered NTP server list:

```console
dsmctl control-panel time capabilities --nas office
dsmctl control-panel time state --nas office --json
```

MCP exposes the same application results through
`get_control_panel_time_capabilities` and `get_control_panel_time_state`.

The compatibility layer selects `SYNO.Core.Region.NTP` v3, then v2, then a
legacy v1 decoder. V1 does not provide the display-format fields, so they are
omitted instead of synthesized. A missing API makes only this module
unsupported; it does not disable storage, SAN, account, or share features.

Guarded time changes use the same hash-bound plan/apply flow as the other
modules. A patch owns only its non-null fields, with one set-replace
exception: `ntp_servers`, when present, is the complete ordered replacement
list. The plan records and hashes the complete current module state; apply
rejects a changed state, merges the patch into a freshly read full
configuration so unspecified fields are never reset, and verifies every
requested field afterward.

Example request:

```json
{
  "time_zone": "Berlin",
  "synchronization_mode": "ntp",
  "ntp_servers": ["time.google.com", "pool.ntp.org"]
}
```

```console
dsmctl control-panel time plan --nas office --file time-change.json --output time-change.plan.json
dsmctl control-panel time apply --file time-change.plan.json --approve <hash-from-plan>
```

Only `SYNO.Core.Region.NTP` v3 has primary `set` evidence, so the set backend
is v3-only; older targets report `set: false` and planning fails closed.
Wall-clock values are never written: `synchronization_mode` accepts only
`ntp`, switching to manual mode is rejected, and a NAS currently in manual
mode accepts a change only when the same patch enables NTP with at least one
server. Time zones must equal the current configuration or resolve in the
embedded IANA database, display formats must use DSM's token grammar such as
`Y-m-d` and `H:i`, and NTP servers are validated for syntax only — neither a
plan nor an apply result ever claims reachability or synchronization
convergence. Time zone changes, enabling NTP, and removing configured servers
are high risk; format-only, append-only, and reorder-only changes are medium.

MCP exposes the same contract through `plan_control_panel_time_change` and
`apply_control_panel_time_plan`.

## SMB and NFS File Services

SMB and NFS are separate compatibility and failure boundaries even though DSM
shows them on the same File Services page:

```console
dsmctl control-panel file-services capabilities --nas office
dsmctl control-panel file-services smb state --nas office --json
dsmctl control-panel file-services nfs state --nas office --json
```

The normalized SMB state contains the service switch, workgroup, minimum and
maximum SMB protocol, transport-encryption policy, and server-signing policy,
plus the advanced "Others" toggles opportunistic locking, SMB2 leases, durable
handles, and local master browser. `disabled_for_smb1` is intentionally distinct
from fully disabled signing: it is the meaning of DSM's value `0`. The advanced
toggles are patch-only booleans applied through the same plan/apply flow (DSM's
SMB set is a partial update, so only the changed fields are sent, alongside the
service-enabled flag). The NFS state contains the service switch, configured
maximum protocol, protocols advertised by DSM, and the NFSv4 domain when the
advanced read API is available.

Base settings use the same hash-bound plan/apply flow for CLI and MCP. A request
owns only its non-null fields; the plan records and hashes the complete current
module state. Apply rejects a changed state, refreshes all normalized fields
before calling DSM, and verifies the requested fields afterward.

Example SMB request:

```json
{
  "protocol": "smb",
  "smb": {
    "minimum_protocol": "smb2",
    "maximum_protocol": "smb3",
    "transport_encryption": "required",
    "server_signing": "required"
  }
}
```

Example NFS request:

```json
{
  "protocol": "nfs",
  "nfs": {
    "enabled": true,
    "maximum_protocol": "nfs4.1"
  }
}
```

```console
dsmctl control-panel file-services plan --nas office --file change.json --output change.plan.json
dsmctl control-panel file-services apply --file change.plan.json --approve <hash-from-plan>
```

MCP exposes `get_file_service_capabilities`, `get_smb_state`, `get_nfs_state`,
`plan_file_service_change`, and `apply_file_service_plan` over the identical
application contract.

DSM's NFS advanced-setting form submits its complete port, packet-size, UNIX
permission, service-state, and domain snapshot in one
`SYNO.Core.FileServ.NFS.AdvancedSetting` set. The NFSv4 domain is writable
through the same file-service plan/apply flow: apply reads the whole advanced
snapshot, overrides only the domain, and resubmits the full snapshot so no
other advanced field is reset. `set_advanced` is `true` only when the advanced
set backend is selected, and a domain change is still planned separately from
the NFS base settings.

```json
{
  "protocol": "nfs",
  "nfs": { "nfsv4_domain": "lab.example" }
}
```

The remaining advanced fields (read/write packet size, custom NFS ports, and the
UNIX-permission switch) are modeled only to preserve them across a domain write;
exposing them as mutations is deferred (WI-025) until their DSM-permitted value
sets are confirmed.

### Per-shared-folder NFS export rules

Each shared folder owns an independent NFS export rule set, exposed separately
from the global NFS switch because it is a different DSM API
(`SYNO.Core.FileServ.NFS.SharePrivilege`) keyed by shared-folder name:

```console
dsmctl control-panel file-services nfs export capabilities --nas office
dsmctl control-panel file-services nfs export list --nas office --share backup --json
```

Each normalized rule has a client pattern (hostname, IP, IP/mask, or a wildcard
such as `*`), a privilege (`read_write` or `read_only`), a squash mapping
(`no_mapping`, `map_root_to_admin`, `map_root_to_guest`, `map_all_to_admin`,
`map_all_to_guest`), a security flavor (`sys`, `kerberos`,
`kerberos_integrity`, `kerberos_privacy`), and the `async`,
`allow_nonprivileged_ports`, and `allow_subfolder_access` switches.

Unlike the patch-only base settings, an export change owns the **complete
desired rule set** for one shared folder: the `rules` array fully replaces the
folder's existing rules, and an empty array removes every rule. The plan records
and hashes the complete observed rule set; apply rejects a changed set, submits
the whole desired set (existing clients as edits, new clients as creations), and
re-reads to verify. Removing a rule, granting read-write to a wildcard client,
or broadening a client from read-only to read-write is high risk.

```json
{
  "share": "backup",
  "rules": [
    {
      "client": "10.0.0.0/24",
      "privilege": "read_write",
      "squash": "map_all_to_admin",
      "security": "sys",
      "async": true,
      "allow_nonprivileged_ports": false,
      "allow_subfolder_access": true
    }
  ]
}
```

```console
dsmctl control-panel file-services nfs export plan --nas office --file export.json --output export.plan.json
dsmctl control-panel file-services nfs export apply --file export.plan.json --approve <hash-from-plan>
```

MCP exposes `get_nfs_export_capabilities`, `get_nfs_export_state`,
`plan_nfs_export_change`, and `apply_nfs_export_plan` over the identical
application contract.

## File Services discovery

The File Services "Advanced" discovery toggles are a separate module because
they are separate DSM APIs from SMB and NFS: Time Machine advertising lives on
`SYNO.Core.FileServ.ServiceDiscovery` (over SMB and AFP) and WS-Discovery on
`SYNO.Core.FileServ.ServiceDiscovery.WSTransfer`. The two are selected
independently, so a backend can expose Time Machine advertising while
WS-Discovery is absent (reported as `(not supported)` and `null`).

```console
dsmctl control-panel file-services discovery capabilities --nas office
dsmctl control-panel file-services discovery state --nas office --json
```

Changes are patch-only through the same hash-bound plan/apply flow: Time Machine
fields are merged into a freshly read pair and submitted as one
`ServiceDiscovery` set, and WS-Discovery is submitted to its own backend.
Disabling an advertisement (which stops client discovery) and enabling
WS-Discovery (which advertises the NAS on the local network) are high risk;
enabling Time Machine advertising is medium.

```json
{ "smb_time_machine": true, "ws_discovery": false }
```

```console
dsmctl control-panel file-services discovery plan --nas office --file discovery.json --output discovery.plan.json
dsmctl control-panel file-services discovery apply --file discovery.plan.json --approve <hash-from-plan>
```

MCP exposes `get_service_discovery_capabilities`, `get_service_discovery_state`,
`plan_service_discovery_change`, and `apply_service_discovery_plan`.

## FTP, FTPS, and SFTP

DSM groups three file-transfer protocols on one "FTP" page, but they are two
independent DSM APIs and two compatibility boundaries: plain FTP and FTP over
explicit TLS (FTPS) share `SYNO.Core.FileServ.FTP`, while SFTP (file transfer
over SSH) is `SYNO.Core.FileServ.FTP.SFTP`. SFTP is selected independently, so a
backend can expose the FTP switches while SFTP is absent (reported as
`(not supported)` and a nil `sftp`).

```console
dsmctl control-panel file-services ftp capabilities --nas office
dsmctl control-panel file-services ftp state --nas office --json
```

The normalized state carries the plain-FTP switch, the FTPS switch, and — when
the SFTP backend is available — the SFTP switch and its listening port. Plain FTP
and FTPS are independent: DSM can serve unencrypted FTP, FTPS, both, or neither.

Changes are patch-only through the same hash-bound plan/apply flow. DSM's FTP set
requires **both** the plain and FTPS switches on every write, so an FTP patch is
merged into a freshly read pair before submitting; the SFTP set requires the
enable switch and always resends the port to preserve it. The plan records and
hashes the complete observed state; apply rejects a changed state, submits the
merged values, and re-reads to verify. Enabling plain FTP (which transmits
credentials without encryption) and disabling a service already in use (which
disconnects clients) are high risk; enabling FTPS or changing the SFTP port is
medium.

```json
{ "ftp": { "ftps": true }, "sftp": { "enabled": true, "port": 22 } }
```

```console
dsmctl control-panel file-services ftp plan --nas office --file ftp.json --output ftp.plan.json
dsmctl control-panel file-services ftp apply --file ftp.plan.json --approve <hash-from-plan>
```

MCP exposes `get_ftp_service_capabilities`, `get_ftp_service_state`,
`plan_ftp_service_change`, and `apply_ftp_service_plan`.

## rsync service

DSM's "rsync" File Services tab enables the rsync network-backup service (used
by remote rsync backups) through `SYNO.Backup.Service.NetworkBackup`.

```console
dsmctl control-panel file-services rsync capabilities --nas office
dsmctl control-panel file-services rsync state --nas office --json
```

The state carries the service switch, the dedicated-rsync-account switch, and the
rsync-over-SSH port. The SSH port is reported **read-only** because DSM shares it
with the SSH daemon; changing it here would move the SSH service, so it is out of
scope for writes. Changes are patch-only through the same hash-bound plan/apply
flow: apply reads the current pair, merges the patch, and submits the service
switch (the write requires it) alongside the account switch. Enabling the service
(which exposes an rsync endpoint), disabling it (which stops incoming backups),
and enabling the rsync account are high risk.

```json
{ "enabled": true, "rsync_account": false }
```

```console
dsmctl control-panel file-services rsync plan --nas office --file rsync.json --output rsync.plan.json
dsmctl control-panel file-services rsync apply --file rsync.plan.json --approve <hash-from-plan>
```

MCP exposes `get_rsync_service_capabilities`, `get_rsync_service_state`,
`plan_rsync_service_change`, and `apply_rsync_service_plan`.

## TFTP service

The TFTP service (`SYNO.Core.TFTP`) is a separate module.

```console
dsmctl control-panel file-services tftp capabilities --nas office
dsmctl control-panel file-services tftp state --nas office --json
```

The state carries the service switch, root folder, permission (`read_only` or
`read_write`), transfer-logging switch, allowed-client IP range, and link
timeout. The allowed-client IP range is reported **read-only** for now (its
bounds interact with an "allow all" flag, so writing them is deferred). The set is a partial update, so only the fields in
the patch are sent. Enabling TFTP requires a root folder, so a patch that enables
the service without one (and with no current root) is rejected at plan time.
Enabling TFTP (an unauthenticated service) and granting write permission (which
lets unauthenticated clients upload) are high risk.

```json
{ "enabled": true, "root_path": "/volume1/tftp", "permission": "read_only", "timeout": 10 }
```

```console
dsmctl control-panel file-services tftp plan --nas office --file tftp.json --output tftp.plan.json
dsmctl control-panel file-services tftp apply --file tftp.plan.json --approve <hash-from-plan>
```

MCP exposes `get_tftp_service_capabilities`, `get_tftp_service_state`,
`plan_tftp_service_change`, and `apply_tftp_service_plan`.

## External Access (read-only)

DSM's Control Panel → External Access surface — the Synology Account (MyDS)
binding, QuickConnect, and DDNS — is exposed as a read-only module under a
dedicated top-level command, because its three areas are separate DSM API
families and separate failure boundaries rather than one settings page:

```console
dsmctl external-access capabilities --nas office
dsmctl external-access account --nas office --json
dsmctl external-access quickconnect --nas office
dsmctl external-access ddns --nas office --json
dsmctl external-access port-forward --nas office
```

The four read areas are selected independently: a NAS can expose QuickConnect
and DDNS while the account read is unavailable, and each reports its own backend
in `capabilities`.

- **Account** reads `SYNO.Core.MyDSCenter` (`query`) and `SYNO.Core.Package.MyDS`
  (`get`): whether a Synology Account is signed in and activated, plus the
  non-secret account identifier, customer id, and serial. The account token
  (`auth_key`) is never decoded into the model, so no display or MCP path can
  leak it.
- **QuickConnect** reads `SYNO.Core.QuickConnect` (`get`, plus v3
  `get_misc_config` for the relay setting and v1 `status` for live connection
  status) and `SYNO.Core.QuickConnect.Permission` (`get`). It reports whether
  QuickConnect is enabled, the QuickConnect ID and region, the relay setting,
  the connection status, the per-service external exposure, and the derived
  relay/direct hostnames. The relay setting and per-service list are null when
  their independently versioned APIs are absent (for example a v1-only NAS).
- **DDNS** reads `SYNO.Core.DDNS.Record` (`list`) and `SYNO.Core.DDNS.ExtIP`
  (`list`): the configured DDNS hostnames and the WAN addresses DSM detected. An
  empty record list means no DDNS hostname is configured.
- **Port forwarding** reads `SYNO.Core.PortForwarding.Rules` (`load`) and
  `SYNO.Core.PortForwarding.RouterConf` (`get`): the configured port-forwarding
  rules and the paired router (brand/model plus UPnP and NAT-PMP support). All
  fields are empty when no router is paired. Rule entries are decoded tolerantly
  (like DDNS records) pending a live sample with a configured rule.

MCP exposes the same reads through `get_external_access_capabilities`,
`get_external_access_account`, `get_external_access_quickconnect`,
`get_external_access_ddns`, and `get_external_access_port_forward`. All are
read-only and never change the account binding, QuickConnect, DDNS, or router
configuration.

### Guarded External Access writes

Every External Access write goes through the same hash-bound plan/apply contract
and is **always high risk** — each changes what the NAS exposes to the public
internet. All plan/apply tools are stripped from the read-only gateway.

- **QuickConnect relay** (`quickconnect plan|apply`) — the relay flag via
  `SYNO.Core.QuickConnect` v3 `set_misc_config` (`relay_enabled`).
- **QuickConnect config** (`quickconnect config plan|apply`) — enable/alias/region
  via `SYNO.Core.QuickConnect` `set`.
- **QuickConnect per-service exposure** (`quickconnect permission plan|apply`) —
  which services are reachable via QuickConnect, through
  `SYNO.Core.QuickConnect.Permission` `set`.
- **DDNS record CRUD** (`ddns plan|apply`) — create/set/delete via
  `SYNO.Core.DDNS.Record`; the provider password uses a `password_ref: env:NAME`
  credential reference resolved only at apply.

```console
echo '{"relay_enabled": false}' | dsmctl external-access quickconnect plan --nas office -o relay.plan.json
dsmctl external-access quickconnect apply --nas office -f relay.plan.json --approve <hash-from-plan>

echo '{"services":[{"id":"dsm_portal","enabled":false}]}' | dsmctl external-access quickconnect permission plan --nas office -o perm.plan.json
dsmctl external-access quickconnect permission apply --nas office -f perm.plan.json --approve <hash>

echo '{"action":"create","provider":"Synology","hostname":"host.synology.me","password_ref":"env:DDNS_PW"}' \
  | dsmctl external-access ddns plan --nas office -o ddns.plan.json
dsmctl external-access ddns apply --nas office -f ddns.plan.json --approve <hash>
```

Each plan records and hashes the complete observed state; apply rejects a changed
state and re-reads to verify the postcondition. Two DSM behaviours the writes
account for (both found live): `SYNO.Core.QuickConnect.Permission.set` rejects a
partial list (error 2901) — the **full** service list is sent, with the patch
merged onto the observed set — and the `services` value is sent as a JSON array,
not a pre-encoded string (which would double-encode). MCP exposes
`plan_/apply_external_access_quickconnect_change`, `…_config_change`,
`…_permission_change`, and `plan_/apply_external_access_ddns_change`.

The relay and per-service exposure writes are live-verified (reverted) on the lab.
The config (enable/alias/region) and DDNS record writes are implemented with their
field names taken from the DSM WebAPI source but are **not live-applied**: the
lab's alias is a real, globally-unique registered id, and DDNS record creation
publishes a real public hostname the lab has no provider account for. The guarded
plan/apply fails closed on a wrong field (postcondition mismatch), so an
unverified field cannot corrupt state. Reaching a NAS *by* its QuickConnect ID is
a separate connection-layer concern in
[WI-042](../spec/work-items/WI-042-quickconnect-transport.md).

## Terminal and SNMP

DSM's Control Panel → Terminal & SNMP page carries two independent DSM API
families with independent failure boundaries: the Terminal tab (SSH/Telnet) is
`SYNO.Core.Terminal` and the SNMP tab is `SYNO.Core.SNMP`. One being absent
reports `(not supported)` without disabling the other, and each fails closed
(no silent empty-success decode) when its API is missing.

```console
dsmctl control-panel terminal-snmp capabilities --nas office
dsmctl control-panel terminal-snmp terminal --nas office --json
dsmctl control-panel terminal-snmp snmp --nas office --json
```

- **Terminal** reads `SYNO.Core.Terminal` (`get`, v3→v2→v1): whether SSH and
  Telnet are enabled, the SSH listening port, and whether local console access
  is forbidden. dsmctl drives DSM over the WebAPI session, not SSH, so these
  describe the human remote-shell exposure only. (The cipher/kex/mac algorithm
  menus DSM returns are ignored.)
- **SNMP** reads `SYNO.Core.SNMP` (`get`, v1): whether the service is enabled,
  which protocol versions (v1/v2c, v3) are on, the device location and contact,
  the SNMPv3 username, and whether a read community and a trap target are
  configured.

**Secret suppression is mandatory on read.** The SNMP `get` echoes the v1/v2c
community string (`rocommunity`), and — when configured — the SNMPv3 auth and
privacy passwords and any trap community, in cleartext. The decoder **never
reads these values into the model**: it surfaces only presence flags
(`community_configured`, `trap_configured`, `trap_host_present`) and the
non-secret SNMPv3 username. A unit test injects a canary into every secret
field and asserts the re-encoded model carries no trace of it. The community
string and passwords are never returned by any read, CLI output, or MCP result.

MCP exposes the same reads through `get_terminal_snmp_capabilities`,
`get_terminal_state`, and `get_snmp_state`. All are read-only and never change
the Terminal or SNMP configuration.

Verified live on DSM 7.3: `SYNO.Core.Terminal` v1–v3 (`enable_ssh`,
`enable_telnet`, `ssh_port`, `forbid_console`) and `SYNO.Core.SNMP` v1
(`enable_snmp`, `enable_snmp_v1v2`, `enable_snmp_v3`, `location`, `contact`,
`rocommunity`, `rouser`).

### Guarded writes

Both areas take patch-only changes through the hash-bound plan/apply contract,
merged into a freshly read state so an unspecified switch is never silently
reset, then re-read to verify the effect. Both plan/apply pairs are excluded
from the read-only MCP gateway.

```console
echo '{"ssh_port":2222}' | dsmctl control-panel terminal-snmp terminal-plan --nas office -f -
dsmctl control-panel terminal-snmp terminal-apply --nas office -f plan.json --approve <hash>

WI071_COMMUNITY=... dsmctl control-panel terminal-snmp snmp-apply --nas office -f plan.json --approve <hash>
```

- **Terminal** set (`SYNO.Core.Terminal` `set`, v3→v2→v1): `ssh_enabled`,
  `ssh_port`, `telnet_enabled`, `console_forbidden`. Enabling SSH or Telnet, or
  disabling SSH, is classified **high** risk — it changes the human remote-shell
  attack surface (dsmctl itself drives DSM over the WebAPI session, not SSH, so
  its own access survives). An SSH-port change is medium and warns to verify the
  matching firewall rule / upstream port forward separately (out of scope here).
- **SNMP** set (`SYNO.Core.SNMP` `set`, v1): `enabled`, `v1_v2c_enabled`,
  `v3_enabled` (disable only), `location`, `contact`, and the read community.
  Every SNMP change is **medium** risk. The read community is a **secret**
  supplied as `community_credential_ref: env:NAME`, resolved to bytes only at
  apply time and sent solely in the SNMP `set` request body (as `rocommunity`);
  the reference NAME — never the community value — is all that enters the plan,
  the approval hash, the result, or a log line. A request-capture unit test
  proves the resolved secret rides only the wire request and is zeroized after.

**WIRE-UNVERIFIED (not writable through this module).** Enabling SNMPv3 requires
a v3 auth passphrase whose DSM `set`-field names could not be confirmed live
(DSM returns error 2202 for every candidate, and the module admin JS was not
fetchable); only *disabling* v3 is supported. The SNMP trap target is likewise
unverified — no trap field appears in the SNMP `get` response even while the
service is enabled, so a trap write cannot be confirmed by a postcondition
re-read. Both are left capability-only pending a codesearch/JS confirmation.

DSM quirks confirmed live and handled: SNMP `set` returns code 2202 when a
required secret is missing (v1/v2c enabled with no community, or v3 enabled with
no passphrase) — the plan pre-checks the community case; and an empty-string
`location`/`contact` is applied only while the service is enabled (DSM ignores an
empty-string write while SNMP is disabled), and DSM has no API to blank a
configured community once set.

## Login Portal (read-only)

DSM's Control Panel → Login Portal page carries three independent DSM API
families with independent failure boundaries: the DSM tab
(`SYNO.Core.Web.DSM`, with the customized-hostname sibling
`SYNO.Core.Web.DSM.External`), the Applications tab (`SYNO.Core.AppPortal`), and
the Advanced tab reverse proxy (`SYNO.Core.AppPortal.ReverseProxy`). One being
absent reports `(not supported)` without disabling the others, and each fails
closed (no silent empty-success decode) when its API is missing.

```console
dsmctl control-panel login-portal capabilities --nas office
dsmctl control-panel login-portal dsm --nas office --json
dsmctl control-panel login-portal applications --nas office --json
dsmctl control-panel login-portal reverse-proxy --nas office --json
```

- **DSM access** reads `SYNO.Core.Web.DSM` (`get`) into stable field names: HTTP
  and HTTPS ports, HTTPS enabled, HTTP→HTTPS force-redirect, HSTS, HTTP/2 (DSM
  field `enable_spdy`), and the customized domain (`enable_custom_domain` /
  `fqdn`). **v1 is selected deliberately**: DSM 7.3 advertises both v1 and v2, but
  the v2 `get` omits `enable_https` and `enable_hsts`, so v1 is the only version
  carrying the complete normalized set. The customized external hostname is an
  independently gated enrichment from the sibling `SYNO.Core.Web.DSM.External`
  (`get` → `hostname`); when that API is absent it is reported `(not supported)`
  without failing the DSM-access read.
- **Applications** reads `SYNO.Core.AppPortal` (`list`) → the per-application
  portal list with app id, title, and per-app HTTP→HTTPS redirect. Alias and
  custom portal ports are surfaced only when a custom portal is configured.
- **Reverse proxy** reads `SYNO.Core.AppPortal.ReverseProxy` (`list`) → the rule
  set, keyed by the server-assigned rule uuid, with the frontend and backend
  (protocol/host/port), the HSTS/HTTP2 frontend flags, whether a certificate is
  referenced, and how many custom headers are configured.

**Secret suppression is mandatory on read.** A reverse-proxy rule may reference a
certificate and carry custom header values (which can hold an injected auth
token). The decoder **never surfaces key material or header values**: it reports
the certificate as presence-only (`certificate_present`) and the headers as a
count-only (`custom_header_count`). A unit test injects certificate/header/SID
canaries and asserts the re-encoded models carry no trace, and a live `--json`
grep confirms no SID/SynoToken leaks.

MCP exposes the same reads through `get_login_portal_capabilities`,
`get_login_portal_dsm`, `get_login_portal_applications`, and
`get_login_portal_reverse_proxy`. All are read-only and never change how DSM is
reached.

Verified live on DSM 7.3: `SYNO.Core.Web.DSM` v1 (`http_port`, `https_port`,
`enable_https`, `enable_https_redirect`, `enable_hsts`, `enable_spdy`,
`enable_custom_domain`, `fqdn`), `SYNO.Core.Web.DSM.External` v1 (`hostname`),
`SYNO.Core.AppPortal` v1 `list` (`id`, `display_name`, `enable_redirect`), and
`SYNO.Core.AppPortal.ReverseProxy` v1 `list` (`entries`). The lab has zero
reverse-proxy rules configured, so the list envelope and rule count are
live-verified but the per-rule field mapping is spec-derived and decoded
leniently (an unknown key yields an empty/zero field, never a wrong value);
re-verifying a populated rule shape is a prerequisite of the guarded-write
follow-on. Guarded writes (DSM ports/HSTS/redirect/domain, application portal
alias/port, reverse-proxy rule CRUD) are a deferred follow-on and are **HIGH
risk** — each changes how DSM itself is reached — and, like the other guarded
modules, will be excluded from the read-only gateway.

## Adding another module

Add a dedicated type under `internal/domain/controlpanel`, an operation package
with strict response decoding and version-scoped variants, and one Synology
facade. Then expose that facade through the shared application service, CLI,
MCP, and compatibility report. Do not add raw DSM response maps or a generic
settings mutation endpoint.
