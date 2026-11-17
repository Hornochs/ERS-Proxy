# Reverse proxy

ERS-Proxy is meant to run behind a reverse proxy that terminates HTTPS. It needs **one host
name and one port**; no path prefix (it must be served at the root of the host).

Three things matter:

1. `public_url` in `config.yaml` is exactly what the browser shows, e.g.
   `https://ers.example.org`. ERS-Proxy compares the `Origin` header of every form submission
   with it and rejects anything else.
2. The proxy passes the `Host` header through and appends the client to `X-Forwarded-For`
   (nginx: `$proxy_add_x_forwarded_for`; Caddy and Traefik do it by default); list the proxy's
   address in `trusted_proxies` so that login lockouts and the audit log see the real client
   address. ERS-Proxy reads `X-Forwarded-For` from the right and skips trusted proxies, so
   entries a client sends itself are ignored; `X-Real-IP` is only a fallback. Without that
   entry every client looks like the proxy, and ERS-Proxy logs a warning on the first such
   request.
3. Responses must not be buffered for too long and request bodies of a few MB must be allowed
   (device import). EDM itself sends small requests.

ERS-Proxy sets all security headers itself (including different Content Security Policies for
the portal and for proxied switch content). **Do not add a global `Content-Security-Policy` or
`X-Frame-Options` header in the reverse proxy** – it would break the workspace tabs or remove
the sandbox of switch content.

## Nginx

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name ers.example.org;

    ssl_certificate     /etc/ssl/ers.example.org.crt;
    ssl_certificate_key /etc/ssl/ers.example.org.key;

    client_max_body_size 16m;

    location / {
        proxy_pass http://127.0.0.1:9050;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 90s;
    }

    # SSH console (only with ssh.enabled): WebSocket, long-lived.
    location ~ ^/ssh/[0-9]+/ws$ {
        proxy_pass http://127.0.0.1:9050;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}

server {
    listen 80;
    server_name ers.example.org;
    return 301 https://$host$request_uri;
}
```

Proxy tokens appear in request paths (`/d/<token>/…`). They are short-lived and bound to a
login session; still, consider shortening access-log retention or logging `$uri` without the
token for `/d/` locations.

The SSH console (`ssh.enabled`) uses a WebSocket below `/ssh/<id>/ws`. Without the second
`location` Nginx answers the upgrade with `426 Upgrade Required` and the console shows
"Cannot connect". ERS-Proxy pings every console every 30 seconds, so the read timeout only has
to be longer than that; 3600 s also covers slow networks.

## Caddy

```caddyfile
ers.example.org {
    request_body {
        max_size 16MB
    }
    reverse_proxy 127.0.0.1:9050
}
```

Caddy passes WebSockets (SSH console) through without further settings.

## Apache

```apache
<VirtualHost *:443>
    ServerName ers.example.org
    SSLEngine on
    SSLCertificateFile    /etc/ssl/ers.example.org.crt
    SSLCertificateKeyFile /etc/ssl/ers.example.org.key
    ProxyPreserveHost On
    ProxyPass        / http://127.0.0.1:9050/
    ProxyPassReverse / http://127.0.0.1:9050/
    RequestHeader set X-Forwarded-Proto "https"
    # SSH console (ssh.enabled): WebSocket; needs mod_proxy_wstunnel or Apache 2.4.47+
    ProxyPass        / http://127.0.0.1:9050/ upgrade=websocket
</VirtualHost>
```

With the SSH console, use the `ProxyPass` line with `upgrade=websocket` instead of the first one.

## Without a reverse proxy

Set `tls.cert_file` and `tls.key_file` and `listen: 0.0.0.0:9050`; `public_url` is then
`https://<host>:9050`. Without any TLS the portal shows a permanent warning: passwords for
ERS-Proxy and for the switches would cross the network unencrypted.
