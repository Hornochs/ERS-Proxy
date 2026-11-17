package store

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 00007 turns former gwn devices into links and resets the removed
// data source and driver.
func TestMigrationRemoveGWN(t *testing.T) {
	ctx := context.Background()
	db, err := Open(Options{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sub, err := fs.Sub(sqliteMigrations, "migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.sql, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][3]string{{"a", "gwn", "gwn"}, {"b", "link", "none"}, {"c", "ers-edm", "snmp"}} {
		drv := "auto"
		if r[0] == "a" {
			drv = "grandstream"
		}
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO devices (name, kind, url, open_mode, isolation, tls_mode, monitor_source, monitor_driver, created_at, updated_at)
			VALUES (?, ?, 'https://x.example', 'inherit', 'sandbox', 'auto', ?, ?, 1, 1)`, r[0], r[1], r[2], drv); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{"a": {"link", "none", "auto"}, "b": {"link", "none", "auto"}, "c": {"ers-edm", "snmp", "auto"}}
	for name, w := range want {
		var got [3]string
		if err := db.sql.QueryRowContext(ctx, `SELECT kind, monitor_source, monitor_driver FROM devices WHERE name = ?`, name).
			Scan(&got[0], &got[1], &got[2]); err != nil || got != w {
			t.Errorf("%s: %v, want %v (%v)", name, got, w, err)
		}
	}
}
