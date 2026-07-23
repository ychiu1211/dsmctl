# Supported Synology matrix

The dsmctl/SPK release version is currently `7.3.2-20`: DSM compatibility train
7.3.2, dsmctl build 20. That managed-NAS compatibility label is separate from
the host DSM installation and hardware matrix below.

Only Synology systems reporting `x86_64` are eligible. ARM, `i686`, DSM 6,
DSM 7.0/7.1, models without Container Manager, and alternate architectures are
deliberately rejected. A separate `DockerEngine` provider present on DSM 7.3
does not replace the required `ContainerManager` package integration. The two
providers must not run concurrently because the `docker-project` worker is
coupled to Container Manager's Compose/API version.

| DSM | Container Manager | Intel x86_64 | AMD x86_64 | Release claim |
| --- | --- | --- | --- | --- |
| 7.2.1-69057 | 1432 or newer | hardware run required | hardware run required | minimum install boundary |
| 7.2.2-72806 | current supported build | hardware run required | hardware run required | candidate, not yet certified |
| 7.3-81168 | 24.0.2-1606 | DS3018xs install/start/stop/upgrade/portal pass; reboot/uninstall pending | hardware run required | partial certification of the named release/model only |

Web Station is also required because the official webservice worker owns the
HTTPS alias portal and reverse proxy. A release may change “hardware run
required” to a named tested model only after offline install, start/stop,
reboot, upgrade, retain uninstall, delete uninstall, and behavior tests are
recorded. CPU architecture alone is not treated as sufficient evidence.

The DS3018xs kernel shipped with DSM 7.3-81168 does not expose the CPU CFS or
PIDs cgroup controllers. The SPK therefore uses the limits that Container
Manager can enforce on every supported x86_64 target: a 256 MiB memory limit
and a bounded 16 MiB `/tmp` tmpfs. Generic Linux deployments retain their
separate CPU and process limits when the host kernel supports those controllers.
