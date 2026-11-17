-- +goose Up
ALTER TABLE devices ADD COLUMN ssh_enabled     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN ssh_port        INTEGER NOT NULL DEFAULT 22;
ALTER TABLE devices ADD COLUMN ssh_login       TEXT    NOT NULL DEFAULT '';     -- '' (by kind), web, own, ask
ALTER TABLE devices ADD COLUMN ssh_profile     TEXT    NOT NULL DEFAULT 'auto'; -- auto, modern, legacy (ers-edm only)
ALTER TABLE devices ADD COLUMN ssh_credentials BLOB;                            -- AES-GCM, bound to id|host|ssh_port
ALTER TABLE devices ADD COLUMN ssh_hostkey     TEXT    NOT NULL DEFAULT '';     -- pinned SHA-256 fingerprint ("SHA256:…")

-- +goose Down
ALTER TABLE devices DROP COLUMN ssh_hostkey;
ALTER TABLE devices DROP COLUMN ssh_credentials;
ALTER TABLE devices DROP COLUMN ssh_profile;
ALTER TABLE devices DROP COLUMN ssh_login;
ALTER TABLE devices DROP COLUMN ssh_port;
ALTER TABLE devices DROP COLUMN ssh_enabled;
