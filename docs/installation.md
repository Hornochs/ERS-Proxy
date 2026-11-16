# Installation

## Packages (recommended)

Every [release](https://github.com/lan-dot-party/ERS-Proxy/releases) has packages for amd64,
arm64 and armv7: `.deb` (Debian, Ubuntu), `.rpm` (Fedora, RHEL, Rocky), `.pkg.tar.zst` (Arch)
and `.apk` (Alpine), plus `SHA256SUMS` and a build provenance attestation.

```bash
V=X.Y.Z
curl -LO https://github.com/lan-dot-party/ERS-Proxy/releases/download/v$V/ers-proxy_${V}_amd64.deb
curl -LO https://github.com/lan-dot-party/ERS-Proxy/releases/download/v$V/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
gh attestation verify ers-proxy_${V}_amd64.deb --repo lan-dot-party/ERS-Proxy   # optional
sudo apt install ./ers-proxy_${V}_amd64.deb
```

The package

- installs `/usr/bin/ers-proxy` and the systemd unit `ers-proxy.service` (hardened: own user,
  read-only system, only `/var/lib/ers-proxy` writable),
- creates the system user `ers-proxy`, `/etc/ers-proxy/config.yaml` and the key
  `/etc/ers-proxy/secret.key` that encrypts stored switch passwords,
- does **not** start the service, so that you can check the configuration first.

```bash
sudo nano /etc/ers-proxy/config.yaml           # listen, public_url (see configuration.md)
sudo ers-proxy user create admin --role admin  # asks for the password
sudo systemctl enable --now ers-proxy
```

Then open `public_url` in the browser, log in, and add devices under **Settings → Devices**
(or import a CSV/JSON file).

Updates: install the newer package; the service restarts and applies database migrations.
Make a backup first: `sudo -u ers-proxy ers-proxy -c /etc/ers-proxy/config.yaml backup`.
Removal keeps `/var/lib/ers-proxy` and `/etc/ers-proxy`; `apt purge` deletes them.

## Docker

```bash
docker run -d --name ers-proxy -p 9050:9050 \
  -e ERSPROXY_PUBLIC_URL=https://ers.example.org \
  -v ers-proxy-data:/var/lib/ers-proxy \
  ghcr.io/lan-dot-party/ers-proxy:latest
docker exec -it ers-proxy ers-proxy user create admin --role admin
```

The container listens on `0.0.0.0:9050` without TLS; put a reverse proxy in front of it
([reverse-proxy.md](reverse-proxy.md)). The secret key is created in the data volume.

### Docker Compose

The repository ships two Compose files with the same service; they differ only in where the
data lives:

| File | Data | Use it when |
|---|---|---|
| [`compose.yaml`](../compose.yaml) | named volume `ers-proxy-data` | You want the simplest setup; Docker manages the data. |
| [`compose.bind.yaml`](../compose.bind.yaml) | local directory `./data` | You want the data visible on the host and easy to back up. The directory must belong to UID 100 / GID 101, the user inside the image. |

Both publish port 9050 on `127.0.0.1` only (for a reverse proxy on the same host; change the
mapping to `"9050:9050"` to expose it directly, without TLS), set `ERSPROXY_PUBLIC_URL` and
restart the container unless you stop it. Edit `ERSPROXY_PUBLIC_URL` to the address the browser
uses, and pin a version tag instead of `latest` for production. The container reaches the
switches through the host's normal network routing; no special network mode is needed.

Named volume:

```bash
docker compose up -d
docker compose exec ers-proxy ers-proxy user create admin --role admin   # first admin
docker compose logs -f ers-proxy
docker compose pull && docker compose up -d                              # update
```

Local directory:

```bash
mkdir -p data && sudo chown 100:101 data      # once, before the first start
docker compose -f compose.bind.yaml up -d
docker compose -f compose.bind.yaml exec ers-proxy ers-proxy user create admin --role admin
```

For the other commands add `-f compose.bind.yaml` as well.

**Backup.** `ers-proxy backup` writes a consistent copy of the database while the server runs
(`VACUUM INTO`) to `backups/ers-proxy-<timestamp>.db` in the data directory, or to the file
you name; it refuses to overwrite an existing file. It does **not** include `secret.key`.

```bash
docker compose exec ers-proxy ers-proxy backup
```

The key `secret.key` lives in the data directory next to `ers-proxy.db` and encrypts the stored
switch passwords: back it up together with the database, and keep both private. A database
backup without the key cannot be used to recover the stored passwords. With the local
directory, back up `./data` (including the key); do not copy `ers-proxy.db` itself while the
container runs, use the backup command above or stop the container first. With the named
volume, copy `secret.key` and the files in `backups/` out of the volume, for example with
`docker compose cp ers-proxy:/var/lib/ers-proxy/secret.key .`.

Behind a reverse proxy, `public_url` must be exactly what the browser shows (see
[reverse-proxy.md](reverse-proxy.md)). The proxy reaches the container over the Docker network
or the published port, so its address as seen by ERS-Proxy usually is not `127.0.0.1`: mount
your own `config.yaml` (the commented line in the Compose files) and list the proxy's address in
`trusted_proxies`.

## From source

```bash
git clone https://github.com/lan-dot-party/ERS-Proxy && cd ERS-Proxy
CGO_ENABLED=0 go build -trimpath -o ers-proxy ./cmd/ers-proxy
./ers-proxy -c config.yaml config init
./ers-proxy -c config.yaml user create admin --role admin
./ers-proxy -c config.yaml server
```

## First device

*Settings › Devices › Add switch* opens one form for every kind of device; choose the kind
there. The section *Credentials* takes the switch login and SNMP access right away (optional;
your own password confirms them once).

- **ERS switch:** kind "ERS switch", address `http://<ip>` or `https://<ip>`. With HTTPS the
  first opening shows the certificate fingerprint; an admin confirms it once. Check what the
  switch supports with `ers-proxy tls-probe https://<ip>`.
- **VSP switch (VOSS):** kind "EDM switch, opened directly", address `https://<ip>`. Its EDM
  works in current browsers, so it is opened directly like a link. For the port dashboard use
  SNMP, or the EDM data source after confirming the certificate on the device's edit page
  (*Check and confirm certificate*) and storing an EDM login there.
- **MikroTik switch with SwOS:** kind "MikroTik SwOS (through proxy)", address `http://<ip>`
  (SwOS has no HTTPS). Enter the web login (`admin` and its password) under *Credentials* when
  adding the device, or later on its edit page – ERS-Proxy logs in for the browser. For the port dashboard choose the source "SwOS
  (web)" (VLANs, duplex, LAGs) or SNMP (ports and traffic only).
- **Any other device:** kind "Link", the full URL of its web interface. It opens directly; if
  the device forbids being embedded, ERS-Proxy opens it in a new browser tab.
