package web

import (
	"strings"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
)

func TestDeviceImportSizeLimit(t *testing.T) {
	h := adminHarness(t)
	// A file just over the limit is refused, not truncated and imported.
	big := "name,url\nfirst,https://first.example/\n" + strings.Repeat("#", devices.MaxImportBytes)
	resp := h.upload("/admin/devices/import", "file", "devices.csv", []byte(big))
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	list, err := h.db.ListDevices(t.Context())
	if err != nil || len(list) != 0 {
		t.Fatalf("devices after an oversized import: %d, %v", len(list), err)
	}
	// A small file still imports.
	h.upload("/admin/devices/import", "file", "devices.csv", []byte("name,url,kind\nfirst,https://first.example/,link\n"))
	list, _ = h.db.ListDevices(t.Context())
	if len(list) != 1 {
		t.Fatalf("devices after a small import: %d", len(list))
	}
}
