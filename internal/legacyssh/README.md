# internal/legacyssh – frozen SSH client for old ERS switches

This directory is a copy of `golang.org/x/crypto/ssh` from **x/crypto v0.58.0**
(BSD-3-Clause, see `LICENSE`), together with the x/crypto internal packages it
needs (`internal/poly1305`, `internal/bcrypt_pbkdf`).

## Why it exists

x/crypto v0.59.0 removed support for `ssh-dss` (DSA) host keys. Avaya/Extreme
ERS switches offer only `ssh-dss` host keys and SHA-1 key exchanges
(`diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1`); the oldest
firmware also offers only `hmac-md5`, which x/crypto never implemented. Without
this copy, the SSH console could not reach those switches.

## Changes against upstream

- package renamed from `ssh` to `legacyssh`, import paths of the copied
  internal packages adjusted;
- `hmac-md5` and `hmac-md5-96` added to the insecure MACs (`mac.go`,
  `common.go`);
- a panic while reading a packet, during key exchange or in the channel
  multiplexer ends that connection with an error instead of the process
  (`recover.go`, used in `handshake.go` and `mux.go`).

Nothing else was changed. The server side, port forwarding and agent code are
still present but never called.

## Rules

- Only `internal/webssh` may import this package, and only for devices of kind
  `ers-edm` (enforced by `TestLegacySSHOnlyForWebSSH` in `internal/webssh`).
  Every other device uses the current `golang.org/x/crypto/ssh`.
- `govulncheck` does not see this copy as x/crypto. When x/crypto publishes an
  advisory for `ssh`, check whether the client code here is affected and patch it.
- Do not "update" the copy to a newer x/crypto release: newer releases do not
  support `ssh-dss`. Fix individual problems instead and list them above.
