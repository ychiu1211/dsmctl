# Download Station

A read-only module for the Synology Download Station package, package-version
gated on the installed `DownloadStation` package like the Photos and Surveillance
modules. It targets the stable, publicly-documented legacy
`SYNO.DownloadStation.*` API (each legacy API is served from its own CGI path,
which the client resolves from the discovered API registry).

```console
dsmctl download capabilities --nas office
dsmctl download service --nas office --json
dsmctl download tasks --nas office
dsmctl download statistics --nas office
```

- **`capabilities`** reports the installed package evidence (installed, version,
  running) and which reads are available, each selected independently. A NAS
  without Download Station — or below the verified 3.0 baseline — fails closed.
- **`service`** reads `SYNO.DownloadStation.Info` (`getinfo` + `getconfig`) and
  `SYNO.DownloadStation.Schedule` (`getconfig`): version, manager flag, default
  destination, eMule and auto-unzip switches, per-protocol (BT/eMule/FTP/HTTP/NZB)
  rate limits in KB/s (0 = unlimited), and the bandwidth schedule.
- **`tasks`** reads `SYNO.DownloadStation.Task` (`list`): each task's id, type,
  title, size, status, destination, and live transfer speed. Task entries are
  decoded tolerantly (a size or speed returned as a quoted string is handled)
  because the verification NAS had no task to populate the list.
- **`statistics`** reads `SYNO.DownloadStation.Statistic` (`getinfo`): the
  aggregate download and upload speed in bytes/s.

MCP exposes the same reads through `get_download_station_capabilities`,
`get_download_station_service`, `get_download_station_tasks`, and
`get_download_station_statistics`. All are read-only.

Field shapes are live-verified on Download Station 4.1.2. Task mutations
(create/pause/resume/delete/edit), the global-config writes, RSS, BT search, and
the richer `SYNO.DownloadStation2.*` API generation are out of scope for this
read module — see
[WI-043](../spec/work-items/WI-043-download-station.md).
