# Compatibility

## Switches (EDM through the proxy)

| Model | Firmware | Transport | Status | Verified how |
|---|---|---|---|---|
| ERS 5632FD (single unit) | 6.6.3.015 | HTTP | works | Hardware test 2026-11-03 (Chromium): login, discovery, Switch Summary, Chassis, Bridge, VLANs incl. Ports tab, MLT/LACP, Spanning Tree, Graph → Chassis, table export, open modes embedded, tab and window, logout; auto-login; EDM data source |
| ERS 5600 series (5698TFD(-PWR), 5650TD(-PWR)), stacks | 6.6.3.015 | HTTP | expected to work | Same firmware and EDM files as the verified 5632FD; stacks not yet tested |

## VSP switches with VOSS (kinds `vsp-edm` and `edm-direct`)

| Model | Firmware | Transport | Status | Verified how |
|---|---|---|---|---|
| VSP 4850GTS | VOSS 7.1.10.0 | HTTPS only (TLS 1.2, RSA key exchange, no secure renegotiation) | works | Hardware test 2026-11-04 in Chromium 156: as `vsp-edm` embedded through the proxy – manual login and auto-login, navigation tree, Chassis and VLAN dialogs, a write (`sysLocation`, reverted), EDM's own logout; as `edm-direct` opened directly in a tab; SNMP and EDM data source of the port dashboard (`voss` driver) |
| VSP 8404 | VOSS 8.10.1.0 | HTTPS only (TLS 1.2, ECDHE with AES-GCM) | works | Hardware test 2026-11-04 in Chromium 156 as `vsp-edm`: auto-login (its login page sets no `auth` cookie – fixed), device view, "Open in EDM" for ports 1/15 and 1/1; SNMP data of the port dashboard (`voss` driver: slots 1 and 2, VLANs, tagging, LLDP). The factory login `rwa` was rejected by EDM; another login works. Configuration changes (phase 8, opt-in) verified over the EDM channel 2026-11-06 from an isolated test instance: port VLAN, port name and state, VLAN create/rename/delete, apply with confirm, revert, automatic revert after the deadline and after a restart, save on confirm (`rcSysActionL1`); hybrid ports; PoE not possible over EDM. SNMP channel not tested |

Two ways to use a VSP: `vsp-edm` serves its EDM through the proxy like an ERS (embedded in the
workspace, auto-login, certificate pinned on the server); `edm-direct` lets the browser open it
directly in a tab (no proxy, no auto-login, the browser asks about the certificate). VOSS's EDM
needs no patches of its own (some ERS path patches also match its files and keep it inside the
proxy path). Differences to ERS switches:

- **Login:** the form calls `encode_vrf()` and posts `encoded=<user>:<password>` (no length
  prefix, no preceding XHR check, no 15-character limit). `edmlogin` tells the two apart by the
  login page.
- **Connections:** the web server resets a connection right after the response headers when
  the request says `Connection: close` (or nothing), so the body is lost. Requests to
  `vsp-edm` and `edm-direct` devices therefore say `Connection: keep-alive`; ERS-Proxy still closes the
  connection after each answer.
- **Web Storage:** the login page keeps the user name in `localStorage` and falls back to a
  cookie when it is unavailable (as in the sandbox); sent to the switch, that cookie makes the
  login fail. The shim provides Web Storage in the sandbox (see `docs/security.md`).
- The web server allows only a few EDM sessions at a time and answers `503 Server Too Busy`
  beyond that until idle sessions expire (about 20–25 minutes on the tested switch). The EDM
  data source keeps one session.

## MikroTik switches with SwOS (kind `swos`)

| Model | Firmware | Transport | Status | Verified how |
|---|---|---|---|---|
| CRS305-1G-4S+ | SwOS 2.18 | HTTP only | works | Hardware test 2026-11-04 in Chromium 156 and Firefox ESR 153, read-only: web interface embedded through the proxy with the stored login (all tabs listed, link table loaded, every request below `/d/{token}/`, no console errors); SNMP and SwOS data source of the port dashboard (`swos` driver) |
| other SwOS switches (CSS610, CSS326, CRS3xx) | SwOS 2.x | HTTP only | expected to work | Same web API; the VLAN tagging of older SwOS versions with a per-port "VLAN Header" setting is not interpreted |

SwOS's web interface works in current browsers; the proxy is there for the login and for one
portal. Differences to EDM:

- **Login:** HTTP Digest (MD5, `qop=auth`) for every data file; the page itself (`/index.html`)
  needs none. ERS-Proxy answers the challenge on the server with the stored web login and signs
  later requests in advance with the last nonce (SwOS keeps the nonce for a long time). The
  browser never gets a `WWW-Authenticate` header, so it shows no login dialog, and its own
  `Authorization` header is never forwarded. Without a stored login the device is not opened.
- **Absolute paths:** the page loads its data with `XMLHttpRequest` from `/sys.b`, `/link.b` …;
  the shim maps such paths below `/d/{token}/` for `swos` devices. The answers are JavaScript
  object literals that the page evaluates with `eval`, which the sandbox allows.
- **Logout:** SwOS's *Logout* posts to `/logout` and reloads the page; through the proxy the
  stored login is used again right away. The proxy session is not reused afterwards.
- **SNMP:** system group, IF-MIB (`ifName` = `ifDescr` = the configured port name), BRIDGE-MIB
  without Q-BRIDGE, MIKROTIK-MIB serial number, firmware version and SFP table. No VLANs, no
  EtherLike, no LLDP. `sysObjectID` is `1.3.6.1.4.1.14988.2`.

## MikroTik devices with RouterOS (kind `routeros`)

| Model | RouterOS | Transport | Status | Verified how |
|---|---|---|---|---|
| CRS310-8G+2S+ | 7.25.1 | HTTPS (self-signed) | works (monitoring, read-only) | Hardware test 2026-11-16, read-only: SNMP (`routeros` driver) and the `routeros` data source (REST API): ports, speed, duplex, VLAN membership (VLAN 1337 untagged on ether1–8 and sfp-sfpplus1, tagged on sfp-sfpplus2), PVID, LLDP neighbour and traffic matched the device; SNMP and REST give the same `ifIndex`; certificate pinned through the edit page; device form, dashboard and switch page in Chromium |
| other RouterOS 7 devices (CRS3xx/5xx, routers) | 7.x | HTTPS or HTTP | expected to work | Same REST API; PoE is not read; on devices whose ethernet ports are not numbered in front-panel order the front-panel numbers can differ |
| RouterOS 6 | 6.x | – | SNMP only | No REST API: use the source `snmp` (no VLAN table, duplex or bonds) |

The kind `routeros` shows WebFig through the proxy (since step 7.17; WebFig's own
`X-Frame-Options: sameorigin` is replaced by the proxy's isolation headers). The user logs in to
WebFig itself; its login is encrypted JavaScript, so there is no auto-login. Verified on the
CRS310 (RouterOS 7.25.1, 2026-11-16, read-only) in Chromium, embedded in the workspace: login,
Quick Set, the Advanced menus (interface list with live rates) and logout; every request stayed
below `/d/{token}/`, and the page looked the same as WebFig opened directly. WebFig itself logs
one script error (`ftype`, "reading 'type'") also when opened directly. Facts seen on the CRS310:

- **WebFig:** absolute paths in HTML attributes, `fetch`, `XMLHttpRequest` and `location`
  navigations; an ES module script; menu files (`.jg`) only with `Accept-Encoding: gzip`
  (`406` otherwise); login data in `sessionStorage`, the session token in `localStorage`.

- **SNMP agent:** system group (`sysObjectID` `1.3.6.1.4.1.14988.1`, `sysDescr` "RouterOS
  <board> <version>"), IF-MIB with 64-bit counters, BRIDGE-MIB, LLDP-MIB, from Q-BRIDGE only
  `dot1qPvid`; no VLAN table, EtherLike or IEEE8023-LAG. `ifIndex` is the interface ID: `lo` = 1,
  then the ethernet ports, then bridges and VLAN interfaces.
- **REST API:** every value is a string; `vlan-ids` can hold lists and ranges; `current-tagged`
  and `current-untagged` list only active ports, so the data source merges them with the
  configured members. Speed and duplex are only in `/interface/ethernet/monitor`. A wrong login
  is answered with `401`.
- **Not verified on hardware:** bonds, PoE models, several bridges, RouterOS before 7.25.
- Configuration changes (phase 8) are not supported for RouterOS.

Other ERS families with the same EDM (Ext JS 2.2) – for example ERS 3500, 4000/4800, 5500 and
5900 – are expected to need the same patches. Patches match file content, not paths or
versions, so they apply wherever the affected code appears. Please report results with the
"Device report" issue template.

## SSH console

| Device | SSH server offers | Profile | Status |
|---|---|---|---|
| ERS 5632FD, 6.6.3.015 | key exchange only `diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1`; host key `ssh-rsa` (SHA-1 signature) | `compat` (`auto` falls back) | works (2026-11-16, Chromium; then still with `legacy`, which the switch does not need): group14-sha1, `ssh-rsa`, `aes128-ctr`, `hmac-sha1`; stored web login; banner, Ctrl-Y, `show sys-info`, `logout` |
| VSP 4850GTS, VOSS 7.1.10.0 | key exchange only `diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1` | `compat` | connection and host key verified (2026-11-16); login not yet (the tried account was rejected) |
| VSP 8404, VOSS 8.10.1.0 | host key only `ssh-rsa`, `ssh-dss` | `compat` | connection and host key verified (2026-11-16); login not yet (the tried account was rejected) |
| Other ERS 5600 units and firmware | as above, some with only `ssh-dss` host keys (OpenSSH needs `-oHostKeyAlgorithms=+ssh-dss`) | `legacy` | `ssh-dss` verified against a fake device only |
| Very old ERS firmware | as above, MAC only `hmac-md5` | `legacy` | HMAC-MD5 tested against a fake device; no such switch at hand |
| MikroTik CRS310-8G+2S+, RouterOS 7.25.1 | current algorithms | `modern` | works (2026-11-16, Chromium): negotiated `mlkem768x25519-sha256`, `rsa-sha2-256`, `aes128-gcm@openssh.com`; stored web login; `/system resource print`, `/quit` |
| Others | current algorithms | `modern` | – |

The ERS CLI starts with a banner and "Enter Ctrl-Y to begin"; press Ctrl-Y in the console.
VOSS uses separate SSH accounts (store an SSH login of its own); ERS and RouterOS accept the
web login. If a device offers no algorithm ERS-Proxy supports, the console names both lists.

## Port dashboard drivers

The port dashboard picks a driver from `sysObjectID` (highest match wins; `generic` is the
fallback). All drivers are read-only.

| Driver | Detection | Data used | Verification |
|---|---|---|---|
| `generic` | any device | IF-MIB, BRIDGE-MIB, Q-BRIDGE-MIB, LLDP, LAG, PoE (RFC 3621) | synthetic fixtures only |
| `ers` | `1.3.6.1.4.1.45.` | standard MIBs plus RAPID-CITY VLANs, MLT and S5-CHASSIS stack units | ERS 5632FD (one unit, 6.6.3.015): ports, VLAN bitmaps, tagging, LLDP, unit data; numbering of stack units 2–8 not yet verified |
| `cisco` | `1.3.6.1.4.1.9.` | standard MIBs plus CISCO-VTP-MIB (VLANs, trunks) and CISCO-VLAN-MEMBERSHIP-MIB (`vmVlan`); Q-BRIDGE fallback without VTP | synthetic fixtures only |
| `voss` | `1.3.6.1.4.1.2272.` (RAPID-CITY: VSP, also older ERS 8000) or `sysDescr` starting with `VSP-` | standard MIBs plus RAPID-CITY VLANs (`rcVlanPortMembers`, PVID, tagging), port speed/duplex (`rcPortTable`) and MLT; front-panel slot/port from `ifName` (`1/1`, ifIndex 192 = 1/1); chassis serial and release from `rcChasSerialNumber`/`sysDescr` | VSP 4850GTS (VOSS 7.1.10.0) over SNMP and EDM: ports, speed/duplex, VLANs, PVID, tagging, LLDP. Hybrid ports (tagging with "untag default VLAN", `rcVlanPortUntagDefaultVlan`) are read since 2026-11-06 (VSP 8404, VOSS 8.10.1.0). MLT membership and channelized ports not yet verified |
| `tplink` | `1.3.6.1.4.1.11863.` | standard MIBs plus TPLINK-DOT1Q-VLAN-MIB (port table: PVID, LAG; VLAN table: names, tagged/untagged port lists as text); front-panel ports from `ifName`; Q-BRIDGE fallback | T1500G-8T and Omada SG2210P (firmware 5.20): ports, PVID, tagging, LAG, LLDP; PoE (status, class, power per port, budget) from TPLINK-POWER-OVER-ETHERNET-MIB, POWER-ETHERNET-MIB as fallback. The SNMP agents are slow and drop requests: a poll takes 30–60 s |

| `swos` | `1.3.6.1.4.1.14988.2` (MikroTik SwOS) or `sysDescr` containing `SwOS` | SNMP: IF-MIB and MIKROTIK-MIB serial/firmware; front-panel number = ifIndex. With the `swos` data source also VLANs, PVID, derived tagging, duplex and LAGs ("Trunk1"…) from SwOS's web API, served as Q-BRIDGE, EtherLike and IEEE8023-LAG objects | CRS305-1G-4S+ (SwOS 2.18) over SNMP and the SwOS data source: ports, speed, duplex, VLANs, PVID, traffic. LAG membership and the tagging of hybrid ports not yet seen on hardware |
| `routeros` | `1.3.6.1.4.1.14988.1` (MikroTik RouterOS) or `sysDescr` starting with `RouterOS` (score 90) | SNMP: IF-MIB (64-bit counters), BRIDGE-MIB, LLDP, `dot1qPvid`, `mtxrSerialNumber`; front-panel number = position among the ethernet ports; model and RouterOS version from `sysDescr`. With the `routeros` data source also VLANs, names, tagging, duplex and bonds from the REST API | CRS310-8G+2S+ (RouterOS 7.25.1) over SNMP and the `routeros` data source: ports, speed, duplex, VLANs, PVID, tagging, LLDP, traffic. Bonds not yet seen on hardware |

Please attach `ers-proxy snmp-capture` output to a device report to get a driver verified.

## Why EDM breaks in modern browsers

EDM's `doAjaxRequest()` reads `response.getResponseHeader.Server`. Ext JS 2.2 builds that map
from `getAllResponseHeaders()`, and modern browsers return header names there in lower case
(`server:`). The lookup yields `undefined` and every AJAX callback throws a `TypeError`; EDM stays
empty after the login or hangs at "Discovering Device". The login and index pages also contain a
browser whitelist (`var supportedBrowser = false;`) that raises an alert in every current
browser.

ERS-Proxy patches the header map so that it also contains lower-case and title-case keys, and
turns the whitelist off. See `internal/edmproxy/patches/`.

## Behaviour of the ERS web server (seen on an ERS 5632FD, 6.6.3.015)

- **HTTPS:** not every installation has it; the tested switch offered HTTP only.
- **One connection per request.** The server closes the connection after every answer, and it
  refuses new connections (closed without answer) for a while after bursts – for example two
  EDM starts within a minute. ERS-Proxy repeats refused reads and keeps images in a per-device
  cache; if EDM still shows "failed in discover…", wait a minute and reopen the device.
- **`Cache-Control: max-age=0`** in a request yields an empty `304 Not Modified`, even for a
  POST; ERS-Proxy does not forward browser cache directives.
- **Login:** a wrong password is answered with `200` and `authError.html` ("Authentication
  Failed!"), not with an error status. After a few failed logins the account is locked for about
  1–2 minutes.
- **EDM bridge:** tables with not-accessible index objects (LLDP) return nothing when the index
  columns are requested; EDM asks for the pseudo column `indexNA` instead, which carries the
  instance ("timeMark.localPort.index").
- **Help:** the EDM help files are not on the switch; they have to be put on a TFTP server (see
  `helpSetup.html` on the switch). Without them, the help window shows an error after about 10 s.
- **SNMP:** `s5ChasComDescr` is a description ("24 SFP plus 8 10Gig XFP …"), `s5ChasComVer` the
  model with hardware revision ("5632FD HW:06"), `s5ChasComRunningSoftwareVer` the software.
  The out-of-band port has ifIndex 55001.

## TLS

The ERS 5600 firmware uses the Mocana TLS stack. Browsers from the time of the firmware
(Firefox 3.x) spoke SSL 3.0 and TLS 1.0, so the switch supports at least TLS 1.0. ERS-Proxy's
legacy profile covers TLS 1.0–1.2 with RSA key exchange and AES-CBC, 3DES or RC4-SHA. Not
supported: SSL 3.0, DHE-only cipher suites, RC4-MD5. `ers-proxy tls-probe https://<switch>`
shows what a switch accepts.

RSA keys below 1024 bits are rejected by Go. If a switch uses one, start ERS-Proxy with
`GODEBUG=rsa1024min=0` (for example in a systemd drop-in) or generate a new key on the switch.

## Browsers

The portal and the proxied EDM are tested with Chromium (156), Firefox (ESR 153) and WebKitGTK
(2.54, the engine of Safari) through Selenium: the ERS EDM against the mock switch, VOSS's EDM
on real VSPs, SwOS on a real CRS305 (Chromium and Firefox). Safari itself adds its own privacy rules (Intelligent Tracking Prevention); they
should not matter because everything runs under the portal's origin, but Safari on macOS has
not been tested yet. Edge (Chromium) is expected to work; please report problems.
