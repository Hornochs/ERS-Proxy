-- +goose Up
CREATE TABLE maps (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    sort       INTEGER NOT NULL DEFAULT 0,
    refresh    INTEGER NOT NULL DEFAULT 15,  -- seconds between updates in the browser
    bg         BLOB,                         -- background image (PNG, JPEG or GIF), NULL = none
    bg_type    TEXT    NOT NULL DEFAULT '',  -- its media type
    bg_width   INTEGER NOT NULL DEFAULT 0,
    bg_height  INTEGER NOT NULL DEFAULT 0,
    bg_hash    TEXT    NOT NULL DEFAULT '',  -- hex SHA-256 prefix, ETag and cache key
    created_at INTEGER NOT NULL,             -- Unix seconds (UTC)
    updated_at INTEGER NOT NULL
);

-- Devices on a map; x and y are fractions (0–1) of the map's width and height.
CREATE TABLE map_nodes (
    map_id    INTEGER NOT NULL REFERENCES maps(id) ON DELETE CASCADE,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    x         REAL    NOT NULL,
    y         REAL    NOT NULL,
    PRIMARY KEY (map_id, device_id)
) WITHOUT ROWID;

-- Links drawn by hand ("manual") and automatic links hidden by an admin ("hidden").
CREATE TABLE map_links (
    id       INTEGER PRIMARY KEY,
    map_id   INTEGER NOT NULL REFERENCES maps(id) ON DELETE CASCADE,
    kind     TEXT    NOT NULL,               -- manual, hidden
    a_device INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    a_if     INTEGER NOT NULL DEFAULT 0,     -- ifIndex, 0 = unknown
    b_device INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    b_if     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX map_links_map ON map_links(map_id);

-- +goose Down
DROP TABLE map_links;
DROP TABLE map_nodes;
DROP TABLE maps;
