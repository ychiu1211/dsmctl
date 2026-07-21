# dsmctl gap inventory

A deduplicated audit of what dsmctl does **not** yet cover, produced 2026-07-20
by a fan-out survey of `spec/roadmap.md`, every `spec/work-items/*.md`, every
`docs/*.md` deferred/out-of-scope note, and a code-module-vs-DSM-surface
comparison, then a completeness-critic pass. Statuses verified against the
roadmap at the time of writing.

This file is the planning source for the greenfield-module program. As each
area ships, move it from here to a work item and mark it `done` on the roadmap.

## A. Open work items (not `done` at audit time)

- **WI-017 (P1, in_progress)** — Gateway x86_64 Synology SPK. Code + local
  verification done; **release blocker** on real-hardware certification
  (install/reboot/upgrade/uninstall/DSM-portal/Container-Manager on Intel and
  AMD across DSM 7.2.1/7.2.2+). 1 of 9 acceptance boxes ticked.
- **WI-041 (P2, in_progress)** — External Access writes: DDNS record CRUD and
  QuickConnect enable/alias/region/per-service exposure. Deferred pending a
  throwaway test hostname and per-session authorization.
- **WI-008 (P2, proposed, no spec)** — Advanced share security: encrypted-share
  keys, WORM, custom Windows ACLs. Blocked on product + key-lifecycle policy.
- **WI-042 (P3, proposed)** — QuickConnect ID as a connection target
  (coordinator resolution; relay/hole-punch as a stretch).
- **WI-010 (P1)** — decomposed into WI-060…064 (structured errors,
  observability, CI matrix, schema stability, artifact signing) by a parallel
  session; umbrella only.

> Correction from the critic pass: WI-045 is `done` (the design shipped); its
> ~13 recorded MCP connection/identity gaps are residual backlog, not an open
> item.

## B. Deferred operations inside shipped modules (mostly deliberate)

- **Storage/SAN**: SSD cache expand/convert (WI-013), RAID migration
  (WI-002, field with no backend), volume shrink / cross-pool move /
  remove-disk; SAN snapshot/clone/replication, iSCSI initiator, Fibre Channel
  targets (WI-005/SAN management — the FC note lives in `docs/san-management.md`).
- **Access/credentials**: effective-access indeterminates for custom Windows
  ACLs / IP rules / Advanced Share Permissions / `homes` (WI-007);
  server-side DSM trusted-device revocation (WI-009, local removal only).
- **File services**: SMB advanced "Others" fields (WI-026); NFS packet
  size / custom ports / UNIX-perm / Kerberos (WI-024/025); FTP advanced +
  anonymous (WI-027); rsync SSH port + TFTP IP-range (WI-028).
- **Packages/log/monitor/time**: beta channel + default volume writes
  (WI-020); local .spk upload install (WI-019); Log Center settings/forward/
  export (WI-018); Resource Monitor alarms/tables/retention (WI-021); manual
  wall-clock set (WI-011).
- **Drive Admin**: team-folder watermark/download-restriction (WI-050, rides
  the shipped Share.set — closest-to-ready); DB-usage recalc + activation set
  (WI-053); connection-kick live exec + data_wipe (WI-054); per-node version
  rollback + view_role impersonation (WI-058); index management (WI-022); DB
  volume migration (WI-031). (Drive Privilege.set is deliberately not exposed —
  access is the account module's application privilege.)
- **Photos/Surveillance/DS/Office/FileStation**: Photos end-user surface
  (WI-030); Surveillance recording/event/camera CRUD + Home Mode schedule
  (WI-034/036); DS RSS/search/eMule (WI-043); Office TTF upload (WI-052);
  FileStation Thumb transport / clear_finished / recursive dir transfer
  (WI-049).
- **Gateway internals**: multi-owner tenancy + fan-out mutation (WI-016);
  OIDC federation + alt grants (WI-048); ARM/multi-arch (WI-014); push/webhook
  approval notifications (WI-038); password recovery / secret export (WI-015);
  dark theme.

## C. Whole DSM areas with no module yet — the greenfield program

Ordered by admin value. **The user has prioritized the Security & Networking
and System Administration groups first** (queued as WI-065…080).

### Security & networking (doing first)

| WI | Area | DSM API family (to live-verify) |
| --- | --- | --- |
| WI-065 | Certificate management (import/renew/Let's Encrypt/service binding) | `SYNO.Core.Certificate.*` |
| WI-066 | Firewall rules | `SYNO.Core.Security.Firewall.*` |
| WI-067 | Account protection / auto-block / DoS / enforced MFA policy | `SYNO.Core.Security.AutoBlock`, `.DoS`, `.AccountActivation` |
| WI-068 | Security Advisor (scans, schedule, results) | `SYNO.Core.SecurityScan.*` |
| WI-069 | Network interfaces / bonding / routing / traffic control | `SYNO.Core.Network.*` |
| WI-070 | Login Portal (DSM port/HTTPS/domain alias, app portals, reverse proxy) | `SYNO.Core.Web.DSM`, `SYNO.Core.PortForwarding`, reverse-proxy APIs |
| WI-071 | Terminal (SSH/Telnet) & SNMP | `SYNO.Core.Terminal`, `SYNO.Core.SNMP` |

### System administration (doing first)

| WI | Area | DSM API family (to live-verify) |
| --- | --- | --- |
| WI-072 | Notification settings (email/SMS/push/webhook targets & rules) | `SYNO.Core.Notification.*` |
| WI-073 | Task Scheduler (scheduled/triggered scripts, service tasks) | `SYNO.Core.TaskScheduler.*` |
| WI-074 | DSM Update & Restore (version check/download/install; config backup/restore) | `SYNO.Core.Upgrade.*`, `SYNO.Core.ConfigBackup.*` |
| WI-075 | Hardware, Power & UPS (power schedule/recovery, fan/beep/LED, UPS) | `SYNO.Core.Hardware.*`, `SYNO.Core.ExternalDevice.UPS` |
| WI-076 | External Devices (USB/printer/eSATA) | `SYNO.Core.ExternalDevice.*` |
| WI-077 | Disk SMART tests & health | `SYNO.Storage.CGI.Smart`, `SYNO.Storage.CGI.HddMan` |
| WI-078 | Directory Services (LDAP/AD join, Directory Server) | `SYNO.Core.Directory.*`, `SYNO.ActiveDirectory.*` |
| WI-079 | KMIP key management | `SYNO.Core.KMIP.*` |
| WI-080 | Universal Search index management | `SYNO.Finder.*` / `SYNO.Core.FileIndexing` |

### Not in this program (recorded for later)

- **Data protection**: Hyper Backup (pulled forward 2026-07-21 as
  [WI-087](work-items/WI-087-hyper-backup.md): reads + guarded run/cancel;
  the rest of the family stays here), Snapshot Replication, Active Backup,
  Cloud Sync, **Synology High Availability (SHA)**, **Shared Folder Sync**
  (server-to-server replication).
- **Virtualization/servers**: Container Manager/Docker, Virtual Machine
  Manager, VPN/DNS/DHCP/WebDAV/Web Station/Mail servers.
- **Collaboration/dev/fleet**: Note Station/Calendar/Contacts/Chat servers,
  MariaDB, CMS/Active Insight.
- **Minor**: Regional Options display language/locale (Control Panel;
  time module covers only zone/format/NTP).

## D. Drive / collaboration native (non-admin) surface

Deliberately deferred — different privilege model and blast radius:

1. Drive end-user Files API (`SYNO.SynologyDrive.Files`) — per-user CRUD.
2. Drive native sharing links (distinct from the shipped FileStation links).
3. Office document content + collaboration internals.
4. Drive file/folder labels.
5. Drive ShareSync / C2 sync (`SYNO.SynologyDriveShareSync.*`).
6. Drive/Office KeyManagement — the one surface with zero prior mention.
