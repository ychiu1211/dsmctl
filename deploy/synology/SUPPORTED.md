# Supported Synology matrix

The dsmctl/SPK release version is currently `7.3.2-1`: DSM compatibility train
7.3.2, dsmctl build 1. That managed-NAS compatibility label is separate from
the host DSM installation and hardware matrix below.

Only Synology systems reporting `x86_64` are eligible. ARM, `i686`, DSM 6,
DSM 7.0/7.1, models without Container Manager, and alternate architectures are
deliberately rejected.

| DSM | Container Manager | Intel x86_64 | AMD x86_64 | Release claim |
| --- | --- | --- | --- | --- |
| 7.2.1-69057 | 1432 or newer | hardware run required | hardware run required | minimum install boundary |
| 7.2.2-72806 | current supported build | hardware run required | hardware run required | candidate, not yet certified |
| newer DSM | current supported build | must be tested per release | must be tested per release | not claimed automatically |

Web Station is also required because the official webservice worker owns the
HTTPS alias portal and reverse proxy. A release may change “hardware run
required” to a named tested model only after offline install, start/stop,
reboot, upgrade, retain uninstall, delete uninstall, and behavior tests are
recorded. CPU architecture alone is not treated as sufficient evidence.
