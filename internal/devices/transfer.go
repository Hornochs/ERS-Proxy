package devices

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// ExportVersion is the format version of JSON exports.
const ExportVersion = 1

// Export is the JSON document written by export and read by import. It never
// contains credentials.
type Export struct {
	Version int            `json:"version"`
	Groups  []ExportGroup  `json:"groups"`
	Devices []ExportDevice `json:"devices"`
}

// ExportGroup is a group in an export.
type ExportGroup struct {
	Name string `json:"name"`
	Sort int    `json:"sort,omitempty"`
}

// ExportDevice is a device in an export.
type ExportDevice struct {
	Name      string `json:"name"`
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	OpenMode  string `json:"open_mode,omitempty"`
	Isolation string `json:"isolation,omitempty"`
	TLSMode   string `json:"tls_mode,omitempty"`
	TLSPin    string `json:"tls_pin,omitempty"`
	Health    *bool  `json:"health,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Sort      int    `json:"sort,omitempty"`
	// Monitoring settings. SNMP credentials are never exported.
	MonitorSource   string `json:"monitor_source,omitempty"`
	MonitorDriver   string `json:"monitor_driver,omitempty"`
	MonitorInterval int    `json:"monitor_interval,omitempty"`
	SNMPVersion     string `json:"snmp_version,omitempty"`
	SNMPPort        int    `json:"snmp_port,omitempty"`
}

// Build collects groups and devices for an export.
func Build(ctx context.Context, db *store.DB) (*Export, error) {
	groups, err := db.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	devs, err := db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := &Export{Version: ExportVersion, Groups: []ExportGroup{}, Devices: []ExportDevice{}}
	for _, g := range groups {
		out.Groups = append(out.Groups, ExportGroup{Name: g.Name, Sort: g.Sort})
	}
	for _, d := range devs {
		h := d.HealthEnabled
		ed := ExportDevice{Name: d.Name, Group: d.GroupName, Kind: d.Kind, URL: d.URL, OpenMode: d.OpenMode,
			Notes: d.Notes, Sort: d.Sort, Health: &h, MonitorSource: d.MonitorSource, MonitorDriver: d.MonitorDriver,
			MonitorInterval: d.MonitorInterval, SNMPVersion: d.SNMPVersion, SNMPPort: d.SNMPPort}
		if d.Proxied() {
			ed.Isolation = d.Isolation
		}
		if d.Upstream() {
			ed.TLSMode, ed.TLSPin = d.TLSMode, d.TLSPin
		}
		out.Devices = append(out.Devices, ed)
	}
	return out, nil
}

// WriteJSON writes an export as indented JSON.
func WriteJSON(w io.Writer, e *Export) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// csvHeader is the column order of CSV exports and imports.
var csvHeader = []string{"name", "group", "kind", "url", "open_mode", "notes"}

// csvDangerous reports whether a spreadsheet would read a cell starting with c
// as a formula.
func csvDangerous(c byte) bool {
	switch c {
	case '=', '+', '-', '@', '\t', '\r':
		return true
	}
	return false
}

// csvEscape protects a text cell against formula injection when the export is
// opened in a spreadsheet: a cell that starts with a formula character (after
// any single quotes already there) gets a single quote in front. All exported
// columns are text (names, groups, URLs, notes), so a legitimate value such
// as "-5" is simply exported as "'-5" and restored by csvUnescape; there are
// no numeric columns that would need the plain form.
func csvEscape(v string) string {
	t := strings.TrimLeft(v, "'")
	if t != "" && csvDangerous(t[0]) {
		return "'" + v
	}
	return v
}

// csvUnescape reverses csvEscape for the same columns: one leading quote is
// removed when the rest, after any further quotes, starts with a formula
// character. Other values starting with a quote are left alone.
func csvUnescape(v string) string {
	if strings.HasPrefix(v, "'") {
		if t := strings.TrimLeft(v, "'"); t != "" && csvDangerous(t[0]) {
			return v[1:]
		}
	}
	return v
}

// WriteCSV writes the devices of an export as CSV (a simple subset).
func WriteCSV(w io.Writer, e *Export) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, d := range e.Devices {
		if err := cw.Write([]string{csvEscape(d.Name), csvEscape(d.Group), csvEscape(d.Kind), csvEscape(d.URL),
			csvEscape(d.OpenMode), csvEscape(d.Notes)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ReadJSON parses an export.
func ReadJSON(r io.Reader) (*Export, error) {
	var e Export
	dec := json.NewDecoder(io.LimitReader(r, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if e.Version != ExportVersion {
		return nil, fmt.Errorf("unsupported export version %d", e.Version)
	}
	return &e, nil
}

// ReadCSV parses a CSV file with the header name,group,kind,url[,open_mode][,notes].
// Column order is taken from the header; kind defaults to ers-edm.
func ReadCSV(r io.Reader) (*Export, error) {
	cr := csv.NewReader(io.LimitReader(r, 4<<20))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("CSV is empty")
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	if _, ok := idx["name"]; !ok {
		return nil, errors.New("CSV needs a header with at least the columns name and url")
	}
	if _, ok := idx["url"]; !ok {
		return nil, errors.New("CSV needs a header with at least the columns name and url")
	}
	col := func(row []string, name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return csvUnescape(strings.TrimSpace(row[i]))
		}
		return ""
	}
	e := &Export{Version: ExportVersion}
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		d := ExportDevice{Name: col(row, "name"), Group: col(row, "group"), Kind: col(row, "kind"), URL: col(row, "url"),
			OpenMode: col(row, "open_mode"), Notes: col(row, "notes")}
		if d.Kind == "" {
			d.Kind = store.KindERS
		}
		if g := strings.ToLower(d.Group); g != "" && !seen[g] {
			seen[g] = true
			e.Groups = append(e.Groups, ExportGroup{Name: d.Group})
		}
		e.Devices = append(e.Devices, d)
	}
	return e, nil
}

// ImportResult summarizes an import.
type ImportResult struct {
	GroupsCreated int
	Created       int
	Skipped       int // same name and address already present
	Errors        []string
}

// Key identifies a device by name (case-insensitive) and address; devices with
// the same key count as the same device when importing or adding in bulk.
func Key(name, url string) string {
	return strings.ToLower(name) + "\x00" + url
}

// Import adds groups and devices. Devices with the same name and URL as an
// existing one are skipped, so importing the same file twice is harmless.
// Invalid rows are reported and skipped; valid rows are still imported.
func Import(ctx context.Context, db *store.DB, e *Export) (*ImportResult, error) {
	res := &ImportResult{}
	groups := map[string]int64{}
	existing, err := db.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range existing {
		groups[strings.ToLower(g.Name)] = g.ID
	}
	ensureGroup := func(name string, sort int) (int64, error) {
		if strings.TrimSpace(name) == "" {
			return 0, nil
		}
		if id, ok := groups[strings.ToLower(strings.TrimSpace(name))]; ok {
			return id, nil
		}
		g := &store.Group{Name: name, Sort: sort}
		if err := db.CreateGroup(ctx, g); err != nil {
			return 0, err
		}
		groups[strings.ToLower(g.Name)] = g.ID
		res.GroupsCreated++
		return g.ID, nil
	}
	for _, g := range e.Groups {
		if _, err := ensureGroup(g.Name, g.Sort); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("group %q: %v", g.Name, err))
		}
	}
	devs, err := db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, d := range devs {
		have[Key(d.Name, d.URL)] = true
	}
	for i, ed := range e.Devices {
		d := &store.Device{Name: ed.Name, Kind: ed.Kind, URL: ed.URL, OpenMode: ed.OpenMode, Isolation: ed.Isolation,
			TLSMode: ed.TLSMode, TLSPin: ed.TLSPin, Notes: ed.Notes, Sort: ed.Sort, HealthEnabled: true,
			MonitorSource: ed.MonitorSource, MonitorDriver: ed.MonitorDriver, MonitorInterval: ed.MonitorInterval,
			SNMPVersion: ed.SNMPVersion, SNMPPort: ed.SNMPPort}
		if ed.Health != nil {
			d.HealthEnabled = *ed.Health
		}
		if err := Normalize(d); err != nil {
			res.Errors = append(res.Errors, "device "+strconv.Itoa(i+1)+" ("+ed.Name+"): "+err.Error())
			continue
		}
		key := Key(d.Name, d.URL)
		if have[key] {
			res.Skipped++
			continue
		}
		gid, err := ensureGroup(ed.Group, 0)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("device %d (%s): group: %v", i+1, ed.Name, err))
			continue
		}
		d.GroupID = gid
		if err := db.CreateDevice(ctx, d); err != nil {
			return res, err
		}
		have[key] = true
		res.Created++
	}
	return res, nil
}
