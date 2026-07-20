# Certificate management

The certificate module reads the Control Panel → Security → Certificate
surface: the installed certificates, their public metadata, and which DSM
services and packages each one serves. It is the first module of the
security/networking greenfield program (see [gap-inventory](../spec/gap-inventory.md)).

This slice is **read-only**. The guarded writes — import a cert + private key,
set the default, bind a service, delete — are modeled in
[WI-065](../spec/work-items/WI-065-certificate.md) but deferred: replacing or
deleting the certificate the DSM desktop presents can break admin TLS,
including the connection dsmctl itself rides, so each write needs explicit
per-operation live authorization before it ships.

## Reads

```console
dsmctl certificate capabilities --nas office
dsmctl certificate list --nas office
dsmctl certificate list --nas office --json
```

- `capabilities` reports whether the certificate read backend was selected and
  the discovered API/version.
- `list` shows each installed certificate: subject CN, whether it is the
  default, expiry (with a computed days-to-expiry hint), whether DSM can renew
  it, whether it is broken, the services bound to it, and the stable id used by
  a future set/bind/delete. `--json` adds the issuer, SANs, key type,
  signature algorithm, and the parsed not-before/not-after Unix times.

The read returns **public certificate metadata only** — the decoder never
carries private-key bytes into the model, and no MCP tool returns key material.

MCP tools: `get_certificate_capabilities`, `get_certificates`.

## DSM backend (verified live on DSM 7.3)

- `SYNO.Core.Certificate.CRT` `list` v1 returns `certificates[]`, each with
  `id`, `desc`, `is_default`, `is_broken`, `renewable`, `key_types`,
  `signature_algorithm`, `issuer`/`subject` (CN/org/country + `sub_alt_name`),
  `valid_from`/`valid_till` (`Jan _2 15:04:05 2006 MST`), `user_deletable`,
  and an inline `services[]` — so one read covers both the inventory and the
  service→certificate bindings; no separate binding call is needed. A
  self-signed certificate carries a `self_signed_cacrt_info` block.

Certificate management is DSM core (not a package), so the operation selects on
the advertised API/version alone and fails closed when the API is absent.

## Deferred (guarded writes)

`SYNO.Core.Certificate.CRT` exposes `set` (set default / bind services /
description), `delete`, and `renew` — all verified present on the lab but
**not** wired, because they change the live DSM certificate. `import` and
`export` are a separate multipart flow (private key via `credential_ref`).
Let's Encrypt issuance (`SYNO.Core.Certificate.LetsEncrypt`) is a non-goal —
it drives an external ACME challenge, not a settings write. See WI-065 for the
full write plan.
