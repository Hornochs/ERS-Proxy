# Vendored xterm.js

Unmodified files from npm, checked against the registry's sha512 integrity:

| File | Package | Version | License |
|---|---|---|---|
| `xterm.js`, `xterm.css` | `@xterm/xterm` | 6.0.0 | MIT (`LICENSE`) |
| `addon-fit.js` | `@xterm/addon-fit` | 0.11.0 | MIT (`LICENSE.addon-fit`) |

Used only by the SSH console page (`/ssh/{id}`, `static/ssh.js`). xterm.js creates `<style>`
elements; the console page allows them with a per-response style nonce that `ssh.js` puts on
them (see `docs/security.md`), not with `'unsafe-inline'`.
