# Roadmap

ERS-Proxy is built in phases. Steps are numbered so that status notes, issues and commits can
refer to them ("step 3.2"). A step is **done** when

- `go vet ./...` and `go test ./...` pass (and CI is green once the repository is pushed),
- the "Current status" section below names it as done,
- [architecture.md](architecture.md) describes any structural change it made, and
- [CHANGELOG.md](../CHANGELOG.md) mentions it under `[Unreleased]`.

To continue the work, read "Current status", take the step under **Next**, implement it,
then update this file, `architecture.md` and the changelog (see [CLAUDE.md](../CLAUDE.md)).

## Current status

_Last updated: 2026-11-16_

| Phase | State |
|---|---|
| 0 – Foundation | done |
| 1 – Core and release pipeline | done |
| 2 – Device management and workspace | done |
| 3 – EDM proxy | done |
| 4 – Health checks | done |
| 5 – Auto-login (experimental) | done (mock only) |
| 6 – Hardware and 1.0 | in progress (6.1 partly) |
| 7 – Port dashboard | done (verified on ERS 5632FD, VSP 4850GTS, T1500G-8T, SG2210P, CRS305 with SwOS, CRS310 with RouterOS; stacks pending) |
| 8 – Switch configuration | in progress on branch `switch-config` (8.1–8.10 and 8.12 done; 8.11 partly: EDM channel verified on a VSP 8404 with VOSS 8.10.1.0; open: SNMP channel, VOSS 7.1; design in [switch-configuration.md](switch-configuration.md)) |
| 9 – SSH console | in progress (9.1–9.3 done; 9.4: ERS 5632FD and CRS310 verified, VSPs open) |
| 10 – Network map | done (against the snmpsim demo devices; not yet checked on hardware) |

**Done:** 0.1–0.3, 1.1–1.5, 2.1–2.6, 3.1–3.6, 4.1–4.2, 5.1–5.2, 7.1–7.17, 8.1–8.10 and 8.12 (branch `switch-config`); 8.11 partly (EDM channel on a VSP 8404).

**Next:** 6.1 – remaining hardware checks: a stack (numbering of units 2–8), Firefox on a real
ERS (verified against the mock and on the VSPs), and
"Open in EDM" for a port on an ERS (verified on the VSP only), SNMPv3 with authPriv on a real
switch (snmpsim cannot test it), see
[hardware-tests.md](hardware-tests.md). Then 6.2.

**Phase 8 (branch `switch-config`):** 8.2–8.5 done, against the in-memory test switch.
8.6 is done against the mock switch, 8.7 against snmpsim, 8.8–8.10 (wiring and web UI)
end to end against the mock switch; 8.12 done. 8.1 (recording EDM's writes) is done on a
VSP 8404 with VOSS 8.10.1.0 (2026-11-06), which also verified 8.6 on hardware. The first run of
8.11 over the EDM channel passed C.1–C.10 on the same switch (C.8 n/a). Next: 8.11, SNMP channel
(needs SNMP write credentials on the switch), then VOSS 7.1 (VSP 4850GTS) – checklist in
section 9 of [hardware-tests.md](hardware-tests.md). ERS switches are not supported by the
planner yet.

**Verified on hardware (2026-11-16, step 7.17):** WebFig of the CRS310-8G+2S+ (RouterOS 7.25.1)
embedded through the proxy in the workspace (Chromium): manual login, Quick Set, Advanced
menus with live rates, logout; no request left `/d/{token}/`. Read-only.

**Verified on hardware (2026-11-16, step 7.16):** MikroTik CRS310-8G+2S+ with RouterOS 7.25.1,
read-only. New device kind `routeros` (WebFig opened directly in a tab: it sends
`X-Frame-Options: sameorigin`), driver `routeros` and data source `routeros` (RouterOS's REST
API, because its SNMP agent has no VLAN table, duplex or bonds). SNMP and REST give the same
ports (`ifIndex` = interface ID), speed and LLDP neighbour; the REST source also VLANs, tagging,
PVID and duplex. Self-signed certificate pinned through the edit page; device form, dashboard
and switch page checked in Chromium. Not seen on hardware: bonds, PoE, several bridges.

**Verified on hardware (2026-11-04, step 7.14):** MikroTik CRS305-1G-4S+ with SwOS 2.18, read-only.
New device kind `swos` (web interface through the proxy, server-side HTTP Digest login), driver
`swos` and data source `swos` (SwOS's web API, because its SNMP agent has no VLANs, duplex or
LAGs). Interface embedded in Chromium and Firefox; SNMP and SwOS source give the same ports and
rates, the SwOS source also VLANs, PVID, tagging and duplex. Not seen on hardware yet: LAGs,
hybrid ports ("VLAN Receive: any"), SwOS PoE models.

**Verified on hardware (2026-11-04, step 7.13):** VSP 4850GTS as `vsp-edm`, embedded through the
proxy in Chromium: auto-login and manual login, dialogs, a write (reverted), EDM's logout. Fixed
on the way: keep-alive for proxied VOSS devices, and the manual login (the sandbox's missing
`localStorage` made VOSS's login page send a fallback cookie to the switch; the shim now
provides Web Storage). "Open in EDM" for a port opens the port's dialog, and reopening a device
reuses its proxy session instead of logging in at the switch again. `edm-direct` devices now always open in a tab (embedded, the browser
dropped their login cookie and hid the certificate warning).

**Verified on hardware (2026-11-04, step 7.12):** VSP 4850GTS with VOSS 7.1.10.0 (HTTPS only).
New device kind `edm-direct` (opened directly; VOSS's EDM needs no proxy) and driver `voss`;
SNMP and EDM data source give the same ports, VLANs, tagging, LLDP and unit. Fixed on the way:
VOSS login variant, `Connection: close` losing answers, `Oid_Conversion_Fail` truncating EDM
tables, "half" duplex on ports without link. MLTs and channelized ports not yet seen on hardware.

**Verified on hardware (2026-11-03, step 7.10):** TP-Link T1500G-8T and Omada SG2210P. Neither
implements Q-BRIDGE; the `tplink` driver now reads TPLINK-DOT1Q-VLAN-MIB (PVID, tagged/untagged
port lists, LAG) and numbers front-panel ports by `ifName`.

**Verified on hardware (2026-11-03, step 6.1, partly):** ERS 5632FD, firmware 6.6.3.015, HTTP
(no HTTPS on that switch): EDM login, discovery and the read-only dialogs through the proxy in
Chromium, table export, open modes embedded and tab, SNMP dashboard data (ports, VLANs, tagging,
LLDP, unit) and rates, health checks, window mode, logout, auto-login (also with a wrong
password) and the EDM data source next to a browser session. Fixed on the way: forwarded `Cache-Control: max-age=0`
(empty 304 on the login submit), refused connections after bursts (retry of reads, image cache),
export window in the sandbox, OOB port and unit columns in the ERS driver, wrong password taken
as a successful login, repeated logins with a rejected password, LLDP through the EDM source. See
[compatibility.md](compatibility.md).

**Verified (2026-11-03, phase 7):** drivers generic, ers, cisco and tplink against synthetic
snmpsim agents (`testdata/snmpsim/gen_monitor.py`); the ERS driver also through the EDM data
source against `internal/mockswitch` serving the same data; poller, rates (wrap, reset, reboot,
discontinuity), history roll-ups and retention in unit tests; dashboard, switch and port pages in
Chromium against the simulator. Nothing in phase 7 has touched real hardware yet.

**Verified (2026-11-03):**

- `go vet`, `go test ./...` and golangci-lint v1.64.8 pass locally. The race detector runs in CI
  only (no C compiler on the development machine).
- Browser test `tools/e2e/edm_e2e.py` in Chromium 156 against `tools/mockswitch` serving the
  original EDM webroot of firmware 6.6.3.015 (gzip on): login, discovery and "Switch Summary"
  load in a workspace tab; the EDM document has an opaque origin, cannot reach the portal frame
  or read portal pages, gets `document.cookie` from the shim; all its requests stay below
  `/d/{token}/`; no console errors.
- HTTPS against the mock with TLS 1.0 only (`TLS_RSA_WITH_AES_128_CBC_SHA`, RSA 1024): the
  certificate confirmation page, automatic switch to the legacy profile, EDM through it.
  `tls-probe` also against `openssl s_server -tls1`.
- GoReleaser 2.12 snapshot build: deb, rpm, Arch and apk packages for amd64, arm64 and armv7;
  contents of the .deb checked. The install tests in containers and the release workflow have
  not run yet (no container runtime locally; repository not pushed).
- Auto-login: against the mock only (success, "205 Change Password").

## Phase 0 – Foundation

- **0.1 Repository documents.** README, CLAUDE.md, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT,
  NOTICE, CHANGELOG, this roadmap and the `docs/` skeleton (architecture, security,
  configuration, installation, reverse proxy, compatibility).
- **0.2 Go module and CLI skeleton.** `go.mod`, cobra CLI with `version` and `config init`,
  YAML configuration with `ERSPROXY_CONFIG`, `log/slog`.
  *Accept:* `ers-proxy config init` writes a valid file that `config.Load` accepts.
- **0.3 CI and repository automation.** `ci.yml` (vet, race tests, build, golangci-lint),
  `security.yml` (CodeQL, dependency review, govulncheck, gitleaks), dependabot, issue and PR
  templates. All actions pinned by commit SHA.

## Phase 1 – Core and release pipeline

- **1.1 Store.** SQLite (modernc, no CGO), goose migrations, first migration (users, sessions,
  groups, devices, settings, audit log), `migrate` and `backup` commands.
- **1.2 Secrets.** AES-256-GCM with associated data, key file outside the database.
  *Accept:* a ciphertext bound to one device/host cannot be opened for another.
- **1.3 Authentication.** Users with roles `admin`/`user`, Argon2id, server-side sessions,
  CSRF, login lockout, `user create|list|passwd|delete`.
- **1.4 Web foundation.** Layout, login/logout, account page, security headers, i18n (en/de),
  insecure-transport banner, `/health`.
- **1.5 Packaging and releases.** GoReleaser (binaries, deb/rpm/Arch/apk, SHA256SUMS,
  multi-arch image), systemd unit, sysusers, install scripts, `packaging/test-install.sh`,
  `release.yml` with install tests on seven distributions and build provenance.

## Phase 2 – Device management and workspace

- **2.1 Groups and devices.** Data model, validation, admin CRUD pages, audit log entries.
  Device kinds: `ers-edm` (proxied EDM) and `link` (direct URL).
- **2.2 Sidebar and tabbed workspace.** Device tree with groups and search on the left, tabs
  with iframes on the right that stay alive while hidden; reload, pop out and close per tab;
  open tabs remembered per browser.
- **2.3 Open modes.** Embedded tab, new browser tab or popup window. Resolution: device →
  user default → global default. Account settings for the user default.
- **2.4 Link devices.** Direct URLs; the management CSP `frame-src` is built from the link
  devices' origins.
- **2.5 Import, export and CLI.** JSON/CSV import and export in the UI, `ers-proxy device
  add|list|import|export`.
- **2.6 Bulk add.** Admin page `/admin/devices/bulk`: shared settings (kind, group, monitoring,
  web login, SNMP access) plus a table of name/address rows, with paste from a spreadsheet;
  all devices are created in one transaction or none. *Done.*

## Phase 3 – EDM proxy

- **3.1 Upstream transport.** HTTP and HTTPS to the switch; TLS mode `auto` (modern first,
  then the legacy profile TLS 1.0–1.2 with RSA key exchange, AES-CBC, 3DES, RC4) remembered
  per device; certificate pinning (trust on first use); tolerant status line parser;
  connection limit per device; `ers-proxy tls-probe`.
- **3.2 Proxy sessions.** `/open/{id}` creates an unguessable token bound to the user session
  and the device and redirects to `/d/{token}/`; server-side cookie jar per token; idle expiry;
  tokens end with the session.
- **3.3 Response pipeline.** gzip, `Location` and `Set-Cookie` handling, CSP `sandbox`,
  CORS for the opaque origin (`Access-Control-Allow-Origin: null`, exposed headers, preflight),
  cookie shim for `document.cookie`.
- **3.4 Patch engine.** Content-based patches (header map in `ext-base.js`, browser
  whitelist, absolute paths), per-device patch report, diagnostics page.
- **3.5 Mock switch and browser test.** `tools/mockswitch` (Go) serving a local EDM webroot,
  Chromium end-to-end test (`tools/e2e/`, runs only locally with `ERSPROXY_EDM_WEBROOT`).
  *Accept:* login, discovery and "Switch Summary" load in the sandbox without console errors;
  a script in the sandbox cannot reach the management UI.
- **3.6 Isolation fallback.** Per-device `isolation: off` for EDM features that break in the
  sandbox, with a clear warning in the UI.

## Phase 4 – Health checks

- **4.1 Poller.** Reachability, `Server` header, certificate fingerprint changes; status dots
  in the sidebar; can be turned off globally and per device.
- **4.2 Embeddability.** Detect `X-Frame-Options`, CSP `frame-ancestors` and mixed content for
  link devices; fall back to "new browser tab" automatically.

## Phase 5 – Auto-login (experimental)

- **5.1 Stored credentials.** Per device, write-only, AES-GCM bound to device and host,
  password re-entry required to set them.
- **5.2 Server-side EDM login.** `GET /`, nonce, `POST /` with `encoded`, form submit, cookie
  into the jar, handling of `205.x` answers. Tested against the mock only until hardware tests
  are approved.

## Phase 6 – Hardware and 1.0

- **6.1 Hardware test.** ERS 5600 (read-only, only after approval by the maintainer):
  `tls-probe`, EDM login, discovery, main dialogs. Results go into
  [compatibility.md](compatibility.md).
- **6.2 Hardening and 1.0.** Documentation review, security review, release 1.0.0.

## Phase 7 – Port dashboard

A modern overview of every port of every monitored switch: link state, speed and duplex, VLANs,
PoE, LAG, LLDP neighbour and traffic with stored history. Data comes from SNMP (read-only) or, for
ERS switches without SNMP, from the EDM HTTP interface. Vendors are added as drivers.
Never write to a device: no SNMP sets, no EDM `setVariables`.

- **7.1 SNMP foundation.** `internal/snmp` (from LANLeecher, read-only API), migration with the
  monitoring fields of devices, encrypted SNMP credentials (v2c/v3, bound to device and host),
  "Monitoring" section in the device form, `ers-proxy snmp-test`, snmpsim test harness
  (`internal/snmptest`, CI installs snmpsim).
- **7.2 Model, readers, driver registry, generic driver.** Common snapshot model; drivers read
  objects by MIB name through a reader; generic driver = IF-MIB, EtherLike, POWER-ETHERNET,
  IEEE8023-LAG, LLDP, Q-BRIDGE; `ers-proxy snmp-capture` (anonymised `.snmprec` for device
  reports).
- **7.3 ERS driver.** `rcVlan*` (the ERS 5600 has no Q-BRIDGE), `rcPortOper*`, `rcMltPortMembers`,
  stack units, ifIndex → unit/port. Synthetic `ers5600.snmprec`. Marked unverified until 6.1.
- **7.4 Poller.** One loop per monitored device, rates from Counter64 deltas (wrap, reset,
  discontinuity), port events, snapshot JSON, diagnostics.
- **7.5 History.** Samples per minute (48 h), 15 minutes (30 days), hour (1 year); roll-up and
  retention job; `monitor.retention.*`.
- **7.6 Dashboard overview.** Switch cards with faceplate per stack unit, colours by speed and
  state, markers for PoE/trunk/LAG/uplink, VLAN highlight, search, live update.
- **7.7 Switch and port pages.** Port table, VLAN table, events, traffic charts, "open in EDM".
- **7.8 EDM data source (ERS, experimental).** Reader over `getTableMibs.jsp`/`getFormMibs.jsp`
  with a long-lived server-side EDM session; at most one request at a time, interval ≥ 60 s.
- **7.9 Cisco driver.** VTP VLANs, access VLAN (`vmVlan`), trunks.
- **7.10 TP-Link driver.** After an `snmp-capture` of a real T1500G (read-only, with approval):
  generic plus model, or TP-Link VLAN OIDs. *Done: TP-Link VLAN OIDs (TPLINK-DOT1Q-VLAN-MIB).*
- **7.11 Documentation.** `docs/monitoring.md`, `docs/drivers.md`, configuration, security.
- **7.12 VSP/VOSS.** Device kind `edm-direct` (EDM opened directly, server-side EDM only for the
  data source), driver `voss`, VOSS login variant and EDM quirks. *Done: verified on a VSP
  4850GTS (VOSS 7.1.10.0).*
- **7.13 VSP through the proxy.** Device kind `vsp-edm`: VOSS's EDM embedded through the proxy
  with auto-login; Web Storage for the sandbox in the shim. *Done: verified on the VSP 4850GTS.*
- **7.14 MikroTik SwOS.** Device kind `swos` (web interface through the proxy, HTTP Digest login
  on the server), driver `swos`, data source `swos` (SwOS's web API for VLANs, duplex and LAGs).
  *Done: verified read-only on a CRS305-1G-4S+ (SwOS 2.18).*
- **7.16 MikroTik RouterOS.** Device kind `routeros` (WebFig opened directly in a tab), driver
  `routeros` (SNMP: `sysObjectID` .14988.1, ports by position), data source `routeros` (REST API
  of RouterOS 7: allow-listed GETs plus `/interface/ethernet/monitor` with `once`). *Done:
  verified read-only on a CRS310-8G+2S+ (RouterOS 7.25.1).*
- **7.17 RouterOS through the proxy.** WebFig embedded under `/d/{token}/` like SwOS: absolute
  paths in attributes and `location` navigations rewritten (`patches.Rebase`), `fetch`, element
  URLs and Web Storage property access in the shim, gzip passed through for WebFig's menu files,
  keep-alive; the user logs in to WebFig (no auto-login). *Done: verified read-only on the
  CRS310-8G+2S+ (RouterOS 7.25.1) in Chromium.*

## Phase 8 – Switch configuration (planned)

Admins change port VLANs, port state and name, VLANs and PoE from the dashboard, with
"commit confirmed" (automatic revert unless confirmed, save to NVRAM only on confirmation).
VSP/VOSS first, through EDM (`setVariables`) and SNMP SET behind one interface. Monitoring
stays read-only; writes live only in `internal/switchcfg`, off unless enabled globally and per
device. Design and details: [switch-configuration.md](switch-configuration.md). Work happens on
the branch `switch-config`.

- **8.1 Recording on a VSP.** Each v1 action once by hand in EDM through the proxy; write
  recipes (objects, encoding, answers, errors, save action) into switch-configuration.md.
  *Done 2026-11-06 on a VSP 8404 (VOSS 8.10.1.0): the VOSS planner now saves on confirm
  (`rcSysActionL1` = 7) and supports hybrid ports.*
- **8.2 EDM session package.** Move session handling out of `edmsrc` into
  `internal/edmsession`, shared by poller and writer (pure refactor). *Done.*
- **8.3 Model and VOSS planner.** `Change`, `Assignment`, `Writer`, `Plan` with inverse and
  expected values, capabilities, guards, port-set encoder; table tests. *Done
  (`internal/switchcfg`, `internal/switchcfg/rcplan`; VOSS specifics confirmed in 8.1).*
- **8.4 Migration 00004, store, `configure.*`.** `config_source`, SNMP write credentials,
  `config_changes`. *Done; `configure.enabled` can only be set in `config.yaml`.*
- **8.5 Manager.** Lock, poller gate, apply, read-back, pending, confirm and save, revert,
  recovery on start; failure-injection tests. *Done (`switchcfg.Manager`, `poller.Hold`,
  `PollNow`, `CollectNow`, `OpenSNMP`).*
- **8.6 EDM writer.** `edmwrite`; the mock switch applies `setVariables` to its data. *Done
  against the mock; verified on a VSP 8404 in 8.1 (empty text as `0x00:`, `snmpErrorIndex`,
  status names).*
- **8.7 SNMP writer.** `snmpwrite`, encrypted write credentials, snmpsim `writecache` tests.
  *Done (UI card for the write credentials follows in 8.8).*
- **8.8 Wiring.** Readers and writers per channel, device form, audit entries. *Done; the
  manager only exists when `configure.enabled` is set.*
- **8.9 Web, read part.** Capabilities, preview bound to the plan hash, change history. *Done.*
- **8.10 Web, write part.** Port and VLAN forms, confirm/revert banner (plain forms; the
  countdown in JavaScript). *Done.*
- **8.11 Hardware test.** VSP only, both channels, unused ports and test VLANs. *Partly: EDM
  channel on a VSP 8404 (VOSS 8.10.1.0), 2026-11-06, C.1–C.10 ok (C.8 n/a). Open: SNMP channel,
  VOSS 7.1.*
- **8.12 Documentation and rules.** security, monitoring, drivers, architecture, CLAUDE.md,
  README, CHANGELOG. *Done (threat T8 in security.md, planner guide in drivers.md, checklist
  section 9 in hardware-tests.md).*

8.2–8.5 need no hardware; 8.1 must be done before 8.6 is finished.

## Phase 9 – SSH console

An SSH console in a workspace tab, for admins, off unless `ssh.enabled` is set in
`config.yaml`. The server holds the SSH connection; the browser shows it with xterm.js over a
WebSocket.

- **9.1 SSH core.** `internal/webssh` (connect, host key pin, login, pseudo terminal, resize),
  algorithm profiles `modern`, `compat` (SHA-1 key exchange, `ssh-rsa`: VSPs, ERS 5600) and
  `legacy`, `internal/legacyssh` (frozen x/crypto/ssh v0.58.0 with `ssh-dss` and
  HMAC-MD5 for old ERS switches), fake devices in `sshtest`. *Done.*
- **9.2 Settings.** `ssh.*` in `config.yaml`, migration `00005_ssh.sql`, SSH card on the edit
  page (enabled, port, login source, own login, algorithms, host key reset). *Done.*
- **9.3 Console.** WebSocket bridge with Origin/CSRF check, host key confirmation, login prompt,
  idle timeout, session limit, audit; page with xterm.js; sidebar action. *Done: browser test
  against a fake ERS SSH server (legacy profile) in Chromium.*
- **9.4 Hardware test.** ERS 5632FD (legacy algorithms), VSP 4850GTS and VSP 8404 (own
  login), CRS310 (web login): login and show commands only. See section 11 of
  [hardware-tests.md](hardware-tests.md). *Partly: ERS 5632FD and CRS310 work (2026-11-16); both
  VSPs connect with `compat`, their login is still to be tested with a working SSH account.*

## Phase 10 – Network map

A NagVis-like map: switches as nodes on a background image, links between them with live
rates.

- **10.1 Data collection.** LLDP chassis IDs and management addresses (`System.ChassisID`,
  `System.BaseMAC`, `Neighbor.ChassisID`, `Neighbor.MgmtAddr`), the learned MAC address table
  (`Snapshot.FDB`, read at most every 5 minutes) and RouterOS `interface/bridge/host`, synthetic test data. *Done.*
- **10.2 Topology.** `internal/topology`: pure function from snapshots to links (LLDP, FDB
  fallback). *Done.*
- **10.3 Storage and background images.** Maps, node positions and background images in the
  database (`00006_maps.sql`; PNG, JPEG or GIF up to 8 MiB, no SVG). *Done.*
- **10.4 Map view with live rates.** Page that draws nodes and links with the rates of the
  linked ports (`/maps/{id}`, `state.json`, `static/map.js`). *Done.*
- **10.5 Editor (admins).** Place nodes, choose the background, draw manual links, hide
  links. *Done.*
- **10.6 Documentation and tests.** Handler tests (roles, upload validation, size), topology
  table tests, browser test against the snmpsim demo devices. *Done.* Open: a check of the
  LLDP/FDB links on real switches (read-only, needs approval); FDB from SwOS and EDM.

## Later

- REST API v1 with tokens and an OpenAPI description.
- One subdomain per device, to proxy modern web UIs generically.
- OIDC or trusted-header authentication.
- Take port traffic from LANLeecher instead of polling the same switches twice.
