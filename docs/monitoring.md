# Port dashboard (monitoring)

The port dashboard shows every port of every monitored switch on one page: link state, speed
and duplex, VLANs, PoE, link aggregation, LLDP neighbour and traffic with stored history. It
**only reads**: polling never sends SNMP SET requests and never writes to a switch through EDM.
Changing switch settings is a separate, optional feature that is off unless enabled in
`config.yaml`; see [switch-configuration.md](switch-configuration.md).

## What you see

- **Overview** (`Port dashboard` in the menu): one card per monitored switch with a faceplate
  per stack unit. Each port is a tile with its number.
  - Colour by state and speed: 10 Gbit/s purple, 1 Gbit/s green, 100 Mbit/s orange, 10 Mbit/s
    red, no link grey, administratively disabled hatched.
  - Markers: yellow dot = PoE delivering power; white bar = trunk or hybrid port; blue frame =
    uplink (the LLDP neighbour announces bridge capability); red outline = error counters
    above zero; dashed border = member of a LAG (ERS: MLT).
  - **Highlight VLAN**: pick a VLAN to dim all ports that are not in it. Untagged members are
    drawn with an outline, tagged members with an inner frame.
  - **Search** by port, alias, LAG name or LLDP neighbour name.
  - Clicking a tile opens the **port panel**: state, speed, duplex, PVID, untagged and tagged
    VLANs, mode, PoE (status, class), LAG, LLDP neighbour, utilisation, error and discard
    counters, time of the last change, and a link to open the switch in EDM. For switches
    served through the proxy (`ers-edm`, `vsp-edm`) EDM then selects the port and opens its
    edit dialog (also from the port page); `edm-direct` devices open without the port.
  - The page updates itself; a card shows the last poll time, uptime, ports up, the PoE
    budget and, after a failed poll, the error.
- **Switch page**: model, firmware and serial number per stack unit, port table, VLAN table
  with member ports, recent events and the notes of the driver (things it could not
  determine).
- **Port page**: details, a traffic chart (in and out, spans 1 h, 24 h, 7 d, 30 d, 1 y) and the
  events of this port.

## Enable it per device

In the device form (admin), section **Monitoring**:

| Field | Meaning |
|---|---|
| Source | `none` (default), `snmp`, `edm`, `swos` or `routeros` (see below). |
| Driver | `auto` detects the driver from `sysObjectID` once per polling loop; or fix `generic`, `ers`, `cisco`, `tplink`, `voss`, `swos` or `routeros`. See [compatibility.md](compatibility.md). |
| Interval | 30–3600 seconds, default 60. |
| SNMP port | Port 161 by default. |

The SNMP host is the host of the device URL. The SNMP version and the credentials are entered
together: when adding a device (optional, section **Credentials** of the form, together with the switch login) or later in the
card **SNMP access** on the edit page. Switching the version there (v2c ↔ v3) replaces the
stored credentials; the form shows only the fields of the chosen version and security level.

- Entering credentials needs the admin's own password (re-authentication, see
  [security.md](security.md)); when adding a device only if SNMP data is entered.
- v2c: the community. v3: user name, security level (`noAuthNoPriv`, `authNoPriv`,
  `authPriv`), authentication protocol (MD5, SHA, SHA-224/256/384/512) and password, privacy
  protocol (DES, AES-128/192/256) and password, optional context name. v3 passwords need at
  least 8 characters. Verified against snmpsim (`internal/snmp/v3_integration_test.go`):
  noAuthNoPriv and authNoPriv (MD5, SHA, SHA-256); authPriv is not verified yet – snmpsim
  cannot decrypt authPriv requests (net-snmp fails against it the same way), and no switch
  with an SNMPv3 user has been tested.
- The card is **write-only**: stored values are never shown again, and setting or deleting
  them requires re-entering your own password.
- The credentials are encrypted and bound to the device, its host and its SNMP port. If you
  change the host or the SNMP port, they are deleted and must be entered again.
- They are never part of a device export.

Changes take effect within a minute; the poller restarts the loop of a device whenever its
monitoring settings or credentials change.

### EDM source (ERS and VSP, experimental)

For ERS and VSP switches whose SNMP agent is not reachable, the source `edm` reads the same MIB
objects through the switch's web interface (`getFormMibs.jsp`, `getTableMibs.jsp`). It

- works only for devices of kind `ers-edm`, `vsp-edm` or `edm-direct` and needs the **stored EDM login**
  of the device; over HTTPS the certificate must be confirmed first (for `edm-direct` on the
  device's edit page);
- keeps one logged-in session on the server and sends one request at a time;
- needs an interval of at least 60 seconds;
- cannot provide PoE or EtherLike duplex (and no Q-BRIDGE data); duplex and speed come from the
  ERS port table instead;
- reads the LLDP tables through EDM's pseudo column `indexNA` (their index objects are
  not-accessible);
- stops logging in after the switch rejected the stored login, until the login is changed (ERS
  switches lock the account after a few failed logins);
- leaves out object names the switch cannot resolve (it answers `Oid_Conversion_Fail`) for the
  rest of the session. VOSS's EDM knows neither `ifName`, `ifType` nor `ifHighSpeed`; the `voss`
  driver then takes slot/port from `ifDescr` and 10G speeds from `rcPortOperSpeed`;
- logs the old session out (`/logout.html`) when the device's address, pin or login changes –
  VSP switches allow only a few web sessions and answer `503 Server Too Busy` beyond that;
- uses only read commands. The request formats come from the EDM code of firmware 6.6.3 and were
  verified on an ERS 5632FD and a VSP 4850GTS (VOSS 7.1.10.0): ports, VLANs, tagging, unit and
  LLDP match the SNMP source. A
  browser session in EDM works at the same time; during an EDM start the switch may answer a
  poll with `HTTP 503` or refuse connections for a few seconds (the next poll works).

Prefer SNMP whenever it is available.

### SwOS source (MikroTik)

SwOS's SNMP agent reports ports, link, speed and traffic, but no VLANs, no duplex and no LAGs.
The source `swos` reads them from the web API that SwOS's own web interface uses (`sys.b`,
`link.b`, `fwd.b`, `vlan.b`, `lacp.b`, `stats.b`). It

- works only for devices of kind `swos` and needs the device's stored **web login** (the same
  one the proxy uses; SwOS has only the account `admin`);
- sends only GET requests for these six files – never `snmp.b` (it holds the SNMP community)
  and never the POSTs SwOS uses to apply settings;
- logs in with HTTP Digest on the server; SwOS keeps no session, so nothing has to be logged
  out;
- names ports like SwOS does (`ifName` = `ETH/BOOT`, `SFP1`, … , `ifAlias` = the configured
  name; with SNMP the configured name is the port name);
- derives the tagging per VLAN, which SwOS does not store: a member port sends its default VLAN
  untagged and other VLANs tagged; a port set to "VLAN Receive: only tagged" is shown as a trunk
  with all VLANs tagged; ports with "VLAN Mode: disabled" get no VLAN data;
- shows LAGs as "Trunk1" … "Trunk15";
- works with an interval of 30 seconds or more.

Verified on a CRS305-1G-4S+ with SwOS 2.18: ports, speed, duplex, VLANs, PVID and traffic match
SNMP and SwOS's own pages. LAGs and PoE were not configured on the tested switch; PoE is not
read.

### RouterOS source (MikroTik)

RouterOS's SNMP agent reports ports, link, speed, traffic, LLDP neighbours and each port's
PVID, but no VLAN table, no duplex and no bonds. The source `routeros` reads everything from the
REST API of RouterOS 7 instead. It

- works only for devices of kind `routeros` and needs the device's stored **web login**; the
  REST API is part of the web server (service `www-ssl`, from RouterOS 7.9 also `www`), so the
  device's address is the WebFig address (shown through the proxy). Confirm a self-signed certificate on the device's
  edit page ("Check and confirm certificate") before the first poll;
- sends `GET` requests for an allow-list of menus only (system identity, resource and
  RouterBOARD, SNMP location, interfaces, ethernet, bonding, VLAN interfaces, bridge, bridge
  ports, bridge VLANs, bridge hosts, IP neighbours) and exactly one command, `/interface/ethernet/monitor`
  with `once` (a `POST` in the REST API, but it only reads link rate and duplex of the ports
  with link) – never `set`, `add`, `remove` or `export`. A test checks the requests;
- numbers ports like the SNMP agent (`ifIndex` = RouterOS's interface ID), so the history
  continues when the source changes between `snmp` and `routeros`; the front-panel number is the
  position among the ethernet ports, `ifName` the interface name, `ifAlias` its comment;
- shows VLANs only for bridges with VLAN filtering (a note names bridges without it), with
  configured and dynamic members of the bridge VLAN table; VLAN names come from VLAN interfaces
  on the bridge, otherwise from the entry's comment. A port that admits only tagged frames has
  no PVID. Members of a bond show the bond's VLANs and PVID;
- shows neighbours from `/ip/neighbor` (LLDP, CDP and MNDP);
- does not retry a rejected login until the stored login or the address changes.

Use an account with read rights only: create a group with the policies `read` and `rest-api`
(plus `api` on versions that require it) and a user in it for ERS-Proxy.

Verified read-only on a CRS310-8G+2S+ (RouterOS 7.25.1): ports, speed, duplex, VLANs, PVID,
tagging, LLDP neighbour and traffic match the device; SNMP and REST give the same ports. Bonds
and PoE were not seen on hardware; PoE is not read. SNMP alone (source `snmp`, driver
`routeros`) shows ports, speed, PVID, LLDP and traffic.

SNMP and the EDM data source use the host of the device's address (`url`) – the same address the
web interface is reached at. For devices whose management address differs from the address of
their web UI, enter the management address as the device's address.

## Device identity, MAC address table and topology

Groundwork for a network map.

- **Chassis IDs.** The snapshot carries the device's LLDP chassis ID (`lldpLocChassisId`
  with its subtype) and the base bridge MAC (`dot1dBaseBridgeAddress`). Every neighbour has
  its chassis ID (`lldpRemChassisId`) and its first IPv4 management address (from the index
  of `lldpRemManAddrTable`). MAC addresses are always lower-case `aa:bb:cc:dd:ee:ff`; chassis
  IDs of another subtype are shown as text (or as a MAC when they are six raw bytes). With
  the address table the poller also reads the interface MACs (`ifPhysAddress`,
  `Snapshot.OwnMACs`): switches send frames from port and VLAN interface MACs, so other
  switches learn those rather than the chassis ID (VSP 4850GTS, 2026-11-17). An all-zero base
  MAC (VOSS 7.1) counts as none.
- **MAC address table (FDB).** `dot1qTpFdbPort` (VLAN ID = fdbId) and, when that is empty or
  has entries on bridge ports that `dot1dBasePortIfIndex` does not list, `dot1dTpFdbPort`; the
  table with fewer unknown ports wins (TP-Link JetStream reports port numbers in Q-BRIDGE but
  ifIndexes in the bridge port table; the T1500G-8T indexes `dot1dTpFdbTable` by VLAN and MAC
  like Q-BRIDGE, which is accepted). When both have no entries, RAPID-CITY's
  `rcBridgeTpFdbPort` (index VLAN.MAC, the value is an ifIndex) is read: VOSS's EDM serves
  only that one. A walk the agent answers out of order (seen on the T1500G while its table
  changes) is tried once more. Entries with status invalid, self or mgmt
  (`dot1q/dot1dTpFdbStatus`), multicast and all-zero addresses are left out. Without
  `dot1dBasePortIfIndex` the bridge port is taken as ifIndex when the switch has that
  interface. These rules come from LANLeecher's FDB reader, which was checked on hardware. At
  most 20000 entries are kept (a note says so). The poller reads the table at most every
  5 minutes per device and keeps the last one in the snapshots of the polls in between
  (`Snapshot.FDBTime` says when it was read). An error only adds a note. On RouterOS (REST
  source) it comes from `interface/bridge/host` (read-only GET, entries of the bridge itself
  are left out). Over EDM (checked 2026-11-17): the ERS 5632FD answers `dot1qTpFdbTable`
  and `dot1dTpFdbTable` when the index comes as `indexNA`; VOSS 7.1 and 8.10 answer
  `rcBridgeTpFdbTable`. Not available: SwOS (not yet), and Cisco only for VLAN 1 (per-VLAN
  tables need `community@vlan`, which is not read).
- **Sources.** SNMP and EDM: all objects above (EDM answers the chassis ID and base MAC as
  `0x…` text, which is decoded). RouterOS REST: neighbour MAC from `/ip/neighbor` as chassis
  ID (subtype 4), FDB from the bridge host table.
- **Topology** (`internal/topology`). Devices are identified by chassis ID and base MAC, then
  by interface MACs that no other device lists, else by name or sysName. An LLDP neighbour gives a link; the far port is found by the port ID or
  description, else from the far device's own LLDP table. LAG members are replaced by the LAG
  interface, so a LAG is one link. For device pairs without an LLDP link, the FDB is used: a
  device A links to B when B's MAC is learned on a port of A (the port with the fewest MACs if
  several), unless B lies behind another device on that port (an LLDP link on the same port;
  A and the other device both learned on the port where B sees A; or the other device has A
  and B on different ports of its own, which also works when B has no address table). The far
  port is where B learned A's MAC.
- **Checking the links.** `ers-proxy snmp-test <id> --collect --fdb` prints a device's own
  MACs and its address table (read-only), to compare the map with the cabling.

## Network maps

Maps (menu **Maps**) show devices as nodes on an optional background image (PNG, JPEG or GIF,
up to 8 MiB) and the links between them from the topology above, with the current rate in
both directions and a colour by utilisation (from the port at the A end; if that device is
not monitored, from the B end). A node shows the poll state (up, error, unknown) or, for
devices without monitoring, the health check. Clicking a monitored node opens its switch page. The
page refreshes every `refresh` seconds (default 15). Maps are also listed in the workspace
sidebar and open there as a tab. Several links between the same two devices are drawn side by
side; the line style shows the source (solid LLDP, dashed MAC tables, dotted by hand).

Admins switch to edit mode to add devices, drag them, upload or remove the background, draw
a manual link between two ports (for example through a device that is not monitored), hide
an automatic link that is wrong, and show hidden links again. Positions are stored as
fractions of the map, so a new background of another size keeps the layout.

## Prepare the switch

ERS-Proxy needs read access only. The commands below are **examples to adapt** to your
firmware and network; check the command reference of your switch.

ERS ACLI (SNMPv1/v2c read-only community; replace the placeholders):

```
enable
configure terminal
snmp-server community <ro-community> ro
```

The exact syntax differs between ERS firmware versions (for example `read-view` options instead
of `ro`); check it with `snmp-server community ?` on the switch. ERS-Proxy has not verified this
example on hardware yet.

Restrict who may use the community, for example to the host of ERS-Proxy (192.0.2.10 in this
example) with the access control features of your firmware, or at least by firewall or by
keeping SNMP in a management network. Do not reuse a read-write community. SNMPv3 is
recommended: create a user with a read-only view and `authPriv`, and enter it in the SNMP
access card.

Other vendors: enable SNMP, create a read-only community or (better) an SNMPv3 user with
authentication and privacy, allow only the ERS-Proxy host (UDP, default port 161), and make sure
the view includes `ifTable`/`ifXTable`, and the Q-BRIDGE, LLDP, IEEE 802.3ad and POWER-ETHERNET
MIBs where the switch has them. A restrictive view hides data; the dashboard then shows fewer
details rather than failing.

## Test it

```bash
# credentials stored for device 3: system group and interface count
ers-proxy -c config.yaml snmp-test 3

# additionally run the detected driver and print the port table
ers-proxy -c config.yaml snmp-test 3 --collect

# ad hoc, without a stored device (v2c; the community comes from the environment, never from
# an argument)
ERSPROXY_SNMP_COMMUNITY='<ro-community>' ers-proxy snmp-test --host 192.0.2.20 --collect
```

Timeout is 5 seconds with one retry. `--collect` prints driver, interface, VLAN and unit
counts, one line per physical port and the driver notes. Do not publish the output without
checking it.

## How the numbers are computed

- **Rates** come from counter deltas between two consecutive polls. 64-bit counters
  (`ifHCInOctets`/`ifHCOutOctets`) are used when the switch has them; otherwise 32-bit
  counters, where one wrap between two polls is accepted.
- A rate is **skipped** (stays 0 for that round) when the device rebooted (uptime went back),
  `ifCounterDiscontinuityTime` changed, the counter source changed between 32 and 64 bit, a
  64-bit counter went backwards, or the result exceeds 1.5 times the line rate (100 Gbit/s if
  the speed is unknown). A reset never produces a spike.
- **Utilisation** is the higher direction in percent of the link speed. A port without link
  has speed 0.
- **Events** are recorded between two snapshots for physical ports: link (oper state),
  admin state, speed (while up), VLAN membership and PoE status.
- **History**: one sample per minute for every physical port and LAG, rolled up to 15-minute and
  hourly values (kept as average and maximum). The charts use the 1-minute data up to 6 hours,
  the 15-minute data up to 8 days and hourly data beyond that. Samples of the same minute
  replace each other.

| Setting | Default | Resolution |
|---|---|---|
| `monitor.retention.minute` | 48 h | 1 minute |
| `monitor.retention.quarter` | 720 h (30 days) | 15 minutes |
| `monitor.retention.hour` | 8760 h (1 year) | 1 hour |
| `monitor.retention.events` | 8760 h | events |

See [configuration.md](configuration.md) for the exact rules and the estimated database size.
Roll-up and deletion run every five minutes.

**History starts with the second poll**, because a rate needs two snapshots. After a restart of
ERS-Proxy the first poll again only provides the baseline.

## Security notes

- **SNMPv2c is plaintext.** The community can be read by anyone on the path. Use a read-only
  community that the switch restricts to the ERS-Proxy host or a management network, and
  prefer SNMPv3 with `authPriv`.
- Credentials are stored encrypted (AES-256-GCM, key in `secret_key_file`), bound to device,
  host and SNMP port, write-only in the UI, and excluded from logs, audit log and exports. See
  [security.md](security.md).
- Monitoring writes nothing to devices. The `internal/snmp` package has no SET API and tests
  (`TestReadOnly`) fail if the SNMP code or the monitor package calls a set function.
  Configuration changes (optional) live in `internal/switchcfg` and use their own write
  credentials; the read-only community here is never used for writing.
- Everything a switch reports (names, aliases, neighbour names) is treated as untrusted and
  escaped in the UI.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Card shows an error, `snmp-test` times out | Wrong address, port or community; the switch's SNMP access list does not include the ERS-Proxy host; a firewall blocks UDP 161. Test from the ERS-Proxy host with `snmp-test`. |
| "no SNMP credentials stored" | Enter them in the SNMP access card. Changing the host or SNMP port deletes them. |
| "No data yet" on a new card | The first poll is running. Charts need a second poll. |
| Wrong or missing details with `auto` | Set the driver explicitly in the Monitoring section (for example `generic` for a switch with standard Q-BRIDGE MIB). Run `snmp-test <id> --collect` and read the notes. |
| VLANs empty on a vendor driver | The switch may not implement the vendor VLAN tables or the SNMP view hides them. Try driver `generic`. A note on the switch page says which read failed. |
| ERS port numbers look wrong | The ERS port numbering (ifIndex blocks of 128 per unit on 5500/5600, otherwise 64) follows EDM code. It and the VLAN bitmaps were verified on a single ERS 5632FD; units 2–8 of a stack are **not yet verified**, and the switch page of a stack carries a note. Please send an `ers-proxy snmp-capture` in a device report. |
| TP-Link: polls take long, "request timeout" now and then | The SNMP agents of JetStream/Omada switches are slow and drop requests; a poll takes 30–60 s. Use an interval of 120 s or more. When a poll takes longer than the interval, the next one waits at least a quarter interval. |
| EDM source: login rejected | The stored EDM login is wrong or lacks rights. The source does not try again (the switch would lock the account); change the login in the device settings, which restarts the session. |
| EDM source: no PoE, duplex missing in places | Not available through EDM. Use SNMP. |
| Traffic is 0 for one round | A reboot, counter reset or implausible delta was skipped on purpose. |
