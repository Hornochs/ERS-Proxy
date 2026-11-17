# Security

This document describes what ERS-Proxy protects, against whom, how, and what remains. Report
vulnerabilities as described in [SECURITY.md](../SECURITY.md).

## Assets

- Access to the management interfaces of network switches (configuration changes can take a
  network down).
- Switch passwords, if stored for auto-login, and SNMP credentials (read, and write if
  configuration changes over SNMP are used).
- The ERS-Proxy accounts and the device list (addresses of management interfaces).

## Threats

| # | Threat | Example |
|---|---|---|
| T1 | Unauthenticated access to the portal | Someone on the network opens the portal |
| T2 | Password guessing | Brute force against `/login` |
| T3 | Cross-site request forgery | A web page makes the browser change devices in the portal |
| T4 | **Malicious switch content** | A compromised switch serves JavaScript that tries to use the portal session, read the device list or control other switches |
| T5 | Theft of stored switch passwords | Copy of the database, or redirecting a device to an attacker's host |
| T6 | Man in the middle between ERS-Proxy and a switch | ARP spoofing in the management network |
| T7 | Leaking switch sessions | Switch cookies end up in the browser or in logs |
| T9 | **Misused SSH console** (only with `ssh.enabled`) | A stolen admin session opens a CLI on a switch; a forged page in the admin's browser drives the console; a device impersonates a switch to collect the login |
| T8 | **Misused or mistaken configuration changes** (only with `configure.enabled`) | A stolen admin session moves an uplink into another VLAN; an admin cuts off the management VLAN by mistake; a forged request applies a change the admin never saw |

## Mitigations

### Portal access (T1–T3)

- Every page except `/login`, `/health` and static files requires a login.
- Passwords: Argon2id (m=19 MiB, t=2, p=1), at least 10 characters, no default account. The
  first admin is created on the command line.
- Login lockout per IP address and per username (5 failures in 10 minutes lock for 5 minutes),
  one uniform error message, constant time for unknown users.
- Sessions are stored server-side as SHA-256 of the cookie value. The cookie is `HttpOnly`,
  `SameSite=Strict`, and `Secure` when `public_url` starts with `https://`. Sessions expire
  after 12 hours of inactivity; changing a password ends all sessions of the user.
- Every state-changing request (POST) needs the session's CSRF token and, if the browser sends
  an `Origin` header, it must match `public_url`. `Origin: null` is always rejected.
- The body of every state-changing request is capped at 16 MiB (413 above), before the CSRF
  check parses the form.
- Security headers on all portal pages: a strict Content Security Policy without inline
  scripts or styles, `frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: same-origin`.

### Switch content isolation (T4)

The proxy has to deliver HTML and JavaScript from the switch to the browser. If that content ran
under the portal's origin, it could use the portal like the logged-in user. ERS-Proxy must work
on a single domain and port (typical reverse proxy setups), so a separate origin is not an
option. Instead:

1. **Browser sandbox.** Every response under `/d/…` carries
   `Content-Security-Policy: sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads`
   – without `allow-same-origin`. The browser gives the document a unique, opaque origin. This
   applies to embedded tabs and to documents opened in a new browser tab or window.
2. **No portal session in the sandbox.** Requests from an opaque origin are cross-site; the
   `SameSite=Strict` session cookie is not sent. The portal additionally rejects
   `Origin: null` and requires the CSRF token.
3. **Unguessable path.** A device is opened through `/open/{id}` (needs a session). ERS-Proxy
   creates a random 128-bit token bound to the user's session and the device and redirects to
   `/d/{token}/`. The token expires after inactivity and with the session. Content of one switch
   cannot learn the token of another switch, so switches are isolated from each other as well.
4. **Cross-origin access only for the token path.** Because the sandboxed document has an opaque
   origin, its XHR requests to `/d/{token}/` are cross-origin. Only these responses carry
   `Access-Control-Allow-Origin: null`; no credentials are involved, the token is the only key.
5. **Framing.** Proxied documents may be framed only by the portal (`frame-ancestors 'self'`)
   and send no referrer.

The sandbox also blocks Web Storage. The shim replaces `localStorage` and `sessionStorage` with
a store that the proxy keeps per proxy session (in memory, at most 64 KiB, dropped with the
session). Only the document of that proxy session can read it (the token is the key), and it is
never sent to the switch. The proxy itself adds only the port to select (`x:port`, an
ifIndex checked as a number).

If a switch's EDM needs a browser feature that the sandbox forbids, an admin can set
`isolation: off` for that device. The portal then shows a permanent warning for the device.

Devices of kind `vsp-edm` (VSP switches with VOSS) and `swos` (MikroTik SwOS) are proxied
exactly like ERS switches. For `swos` devices ERS-Proxy also logs in on the server: SwOS uses
HTTP Digest, and the proxy answers the switch's challenge with the stored web login. The
browser's own `Authorization` header is never forwarded and the switch's `WWW-Authenticate`
header never reaches the browser, so neither a browser login dialog nor the password is
involved in the browser. Content of the sandboxed SwOS page can use the stored login only for
requests to its own switch below its own token path – which is what opening the device grants
anyway. Without a stored login a `swos` device is not opened.

Devices of kind `routeros` (MikroTik RouterOS) show WebFig through the proxy in the same
sandbox. The user logs in to WebFig itself: WebFig encrypts its protocol in the browser
(`/jsproxy`, Curve25519), so the proxy only passes encrypted bytes and there is no auto-login;
the stored web login is used only by the server for the REST data source. WebFig's login page
puts the typed user name and password into `sessionStorage` until WebFig has read them. In the
sandbox that storage is the shim's, so for that moment the values are also held in the proxy
session's page storage (in memory, never sent to the device, never logged); WebFig clears
`sessionStorage` right after the login and the next snapshot replaces them. WebFig keeps its
session token in `localStorage`, which the shim holds in the proxy session the same way, until
the proxy session ends.
Devices of kind `link` and `edm-direct` (VSP switches opened directly) are not proxied: the browser loads them from their own origin, never under the portal's, so this
section does not apply to them. For `edm-direct` devices ERS-Proxy only reads from the EDM
itself (EDM data source) with the pinned certificate and the stored login (T5, T6).

### Stored switch passwords (T5)

- Storing a switch login or SNMP credentials (and creating users) needs the admin's own
  password again (re-authentication). An unattended browser or a stolen session cookie alone
  cannot add or swap stored secrets; failed confirmations count towards the login lockout.
- Encrypted with AES-256-GCM. The key lives in a separate file (`secret_key_file`), so a copy of
  the database alone reveals nothing.
- The ciphertext is bound to the device ID, host and port as associated data. Changing the
  host of a device makes the stored password unusable; it cannot be redirected to another host.
- Password fields are write-only: they are never shown, returned or logged. Setting one requires
  re-entering the user's own password.

### Stored SNMP credentials (T5)

- The v2c community or v3 login is encrypted like the switch password (AES-256-GCM, separate key
  file) and bound to the device ID, host and SNMP port as associated data. Changing the host or
  the SNMP port makes it unusable; the portal deletes it then.
- The form is write-only: values are never shown, returned, logged, audited or exported.
  Setting or clearing requires re-entering the user's own password.
- **SNMPv2c sends the community unencrypted.** Use a read-only community that the switch
  restricts to this host, and prefer SNMPv3 with `authPriv`.
- These read credentials are only used for reading: `internal/snmp` has no SET API (checked
  by a test). Configuration changes over SNMP use separate write credentials (see T8), which
  exist only when an admin stores them.
- SNMP write credentials are stored the same way (AES-256-GCM, bound to device ID, host and
  SNMP port with their own associated data, so read and write ciphertexts cannot be swapped),
  write-only, need the admin's password, and are deleted when the host or SNMP port changes.

### Connection to the switch (T6)

- HTTPS is preferred. Switches use self-signed certificates, so ERS-Proxy pins the SHA-256
  fingerprint of the certificate when the device is added (trust on first use, confirmed by the
  admin). A different certificate blocks the connection until an admin confirms it again.
- TLS mode `auto` tries modern TLS first and falls back to the legacy profile (TLS 1.0–1.2,
  RSA key exchange, AES-CBC, 3DES, RC4) only if the switch supports nothing better. The device
  page shows the negotiated version and cipher and warns below TLS 1.2.
- With HTTP the connection is unencrypted, including the EDM login. The portal marks such
  devices. SwOS speaks HTTP only; its Digest login does not send the password itself, but MD5
  Digest can be attacked offline from a captured exchange – keep SwOS switches in a management
  network.
- The SwOS data source sends only GET requests for six read-only files and never asks for
  `snmp.b` (which contains the SNMP community); a test checks that the reader contains no write
  methods.
- The RouterOS data source sends only GET requests for an allow-list of menus and one read-only
  command (`/interface/ethernet/monitor` with `once`); everything else is refused in the client
  before it is sent, and a test checks the requests. The REST API uses HTTP Basic
  authentication on every request: use HTTPS with a pinned certificate, and a RouterOS account
  with the policies `read` and `rest-api` only.

### Configuration changes (T8)

Changing switch settings from the dashboard ([switch-configuration.md](switch-configuration.md))
is off by default. Port monitoring alone never writes to a switch.

- **Opt-in in two places.** `configure.enabled: true` must be set in `config.yaml`; no
  environment variable, command-line flag or UI setting can turn it on (a test checks the
  environment). Without it the server registers no configuration routes, shows no controls
  and does not start the manager, which also refuses every operation itself. In addition each
  device needs a configuration channel (`config_source`: EDM or SNMP); setting it needs the
  admin's password.
- **Admins only**, through the same protections as every admin action: session, CSRF token,
  same-origin check (`Origin: null` is rejected). Nothing in a switch's sandboxed EDM can reach
  these routes.
- **What is written is what was shown.** A preview is bound to the user and to a hash of the
  planned writes; applying takes only the preview ID and the hash. Before writing, the values
  are read again; if the switch changed since the preview, nothing is written.
- **Guards.** Warnings for uplinks (LLDP neighbour), LAG/MLT members and the management VLAN
  need an explicit acknowledgement; when the management VLAN cannot be determined (EDM), every
  change that could cut the switch off needs it. Deleting VLAN 1 or the management VLAN is
  refused.
- **Commit confirmed.** Every change is read back and stays pending until it is confirmed;
  otherwise it is reverted at the deadline, also after a restart of ERS-Proxy (the plan and
  the previous values are stored before the first write). The configuration is saved on the
  switch only after confirmation, so a reboot restores the previous state if a change cut the
  switch off.
- **Contained code.** Only `internal/switchcfg` writes to switches; tests check that only the
  web layer and the server command import it, and that gosnmp's `Set` is called only in its
  SNMP writer. EDM writes are never retried automatically.
- **Audit.** Apply, confirm, revert (also automatic ones, as `system`) and failures are in the
  audit log with a readable summary, never with credentials; each switch also has a change
  history.

### SSH console (T9)

Off unless `ssh.enabled: true` is set in `config.yaml`; no environment variable or UI turns it
on. Without it no console route exists (404). With it:

- **Admins only.** All console routes are in the admin group; devices offer the console only
  when SSH is turned on in their settings. Other users never see it.
- **No forged connections.** The WebSocket upgrade is a GET, which the CSRF middleware does
  not check, so the handler itself requires a same-origin `Origin` (no `null`, never missing)
  and the session's CSRF token in the query, and the WebSocket library checks the origin again.
  The console page is portal code (no switch content); it may be framed by the portal only
  (`frame-ancestors 'self'`).
- **Host keys are pinned (trust on first use).** The SSH client checks the device's host key
  before it sends the login. An unknown key is shown to the admin with a one-time nonce
  (10 minutes, bound to device and admin); only that nonce pins it. A changed key ends the
  connection with a warning and an audit entry; it can only be reset on the device's edit page.
- **Logins.** Stored SSH logins are AES-GCM encrypted with associated data bound to device, host
  and SSH port (`devices.SSHAAD`); they are cleared when the address or the SSH port changes.
  ERS and RouterOS use the stored web login by default, VOSS a login of its own. A login typed
  in the console is used for that connection only: it is not stored on the server; the page
  keeps it in memory (not in Web Storage) until it is closed, so that confirming a host key does
  not ask again. After three rejected logins the console stops.
- **Nothing is recorded.** Input and output are relayed only. The audit log notes
  `ssh.open` (algorithms, whether the login was stored or typed), `ssh.close` (reason,
  duration, bytes), `ssh.login_failed`, `ssh.hostkey.trust`, `ssh.hostkey.mismatch` and
  `ssh.hostkey.clear`; never passwords or terminal content.
- **Limits.** `ssh.max_sessions` consoles at the same time, an idle timeout
  (`ssh.idle_timeout`), and the console ends when the portal session ends (checked every 30 s).
  The server pings the browser every 30 s.
- **Old algorithms, in two steps.** Many switches only offer weak algorithms: VSPs with VOSS
  and the ERS 5632FD only SHA-1 key exchanges (`diffie-hellman-group14-sha1`,
  `diffie-hellman-group1-sha1`) and `ssh-rsa` host keys signed with SHA-1. The profile
  `compat` adds the current `golang.org/x/crypto/ssh`'s own insecure algorithms for them (SHA-1
  key exchanges, `ssh-rsa`, CBC ciphers) – still the maintained library. Only `ssh-dss` host keys
  and `hmac-md5`, which some older ERS firmware needs, are not in the current x/crypto at all.
  For those ERS-Proxy carries a frozen copy of `x/crypto/ssh` v0.58.0 with HMAC-MD5 added
  (`internal/legacyssh`, see its README), the profile `legacy`. It is used **only for devices
  of kind `ers-edm`**, and with the profile `auto` only after `modern` and `compat` found no
  common algorithm. A test ensures that only the legacy dialer imports it. A panic while it
  parses data from a device ends that connection, not the process. The copy gets no automatic
  security fixes (govulncheck does not see it as x/crypto, and CodeQL findings in it are
  filtered out before the upload, since they are upstream code; the secret scan skips it too):
  check x/crypto advisories for `ssh` and patch the copy when its client code is affected. The algorithms of both steps are weak
  (SHA-1, CBC, DSA-1024); the host key pin protects against a later impersonation, not the first
  contact. Keep such switches in a management network.
- **The terminal library** (xterm.js, MIT, vendored unmodified in `static/vendor/xterm/`)
  creates `<style>` elements. The console page allows styles with a nonce that is new for every
  response (`style-src 'self' 'nonce-…'`) and that `ssh.js` puts on those elements; no
  `'unsafe-inline'`. Terminal output is rendered as text by xterm.js, never as HTML.

### Network maps

- Everyone logged in can view maps; creating, editing, uploading and deleting are in the admin
  group (role, CSRF, same origin) and audited (`map.*`).
- Background images are checked by decoding their header (`image.DecodeConfig`), not by name
  or declared type: only PNG, JPEG and GIF, at most 8 MiB and 50 megapixels. SVG is refused,
  because it can carry script. The image is served with its detected type,
  `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`.
- Device names, port names and errors reach the page only through `textContent` and SVG
  attributes; the map page needs no inline script or style.

### Switch sessions (T7)

- Switch cookies are kept in a per-token cookie jar on the server. They are never sent to the
  browser and never forwarded between devices. EDM reads one cookie through `document.cookie`;
  ERS-Proxy provides it through a small script for that document only.
- Browser cookies (including the portal session) are removed from requests to the switch.

## Residual risks

- **Tokens in URLs.** Proxy tokens appear in the reverse proxy's access log and, with "new
  browser tab", in the browser history. They are short-lived and bound to a session; restrict
  access to the logs.
- **Legacy TLS.** TLS 1.0 with RC4 or 3DES is weak. Use a management network.
- **Users can do what EDM allows.** ERS-Proxy does not restrict what a logged-in user does
  inside a switch's EDM; that is decided by the switch account. SwOS has only one account with
  full rights, so every portal user who can open a `swos` device can change that switch.
- **`isolation: off`** removes the protection against T4 for that device.
- **SSH console** (with `ssh.enabled`): an admin account gives a CLI on every device with SSH
  turned on, with the stored logins. VSPs and ERS switches need weak SSH algorithms (`compat`);
  the oldest ERS firmware even a frozen copy of x/crypto/ssh (`legacy`, see T9).
- **Configuration changes** (with `configure.enabled`): an admin account can change switches
  through ERS-Proxy, so its password protects the network. A change that cuts ERS-Proxy off
  from the switch cannot be reverted by ERS-Proxy; only a restart of the switch restores the
  saved configuration. Confirming a change saves the whole running configuration, including
  unsaved changes others made on the switch.
