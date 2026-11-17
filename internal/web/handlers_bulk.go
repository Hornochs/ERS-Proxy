package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// maxBulkRows is the most devices one bulk request may create.
const maxBulkRows = 200

// initialBulkRows is the number of empty rows on a fresh bulk page.
const initialBulkRows = 5

// bulkRow is one row of the bulk table. Kind is empty for "shared"; Group is
// empty for "shared", otherwise a group ID ("0" = ungrouped).
type bulkRow struct {
	Name, URL, Kind, Group string
	// Err is the translated error of the row (empty = fine).
	Err string
	// Num is the 1-based row number as submitted.
	Num int
}

type bulkData struct {
	// Device carries the shared settings (the snmp_fields partial reads it).
	Device *store.Device
	Groups []*store.Group
	Rows   []bulkRow
	// EmptyRow is the blank row of the client-side row template.
	EmptyRow bulkRow
	// ErrKey is a device.err key of a page-level problem.
	ErrKey string
}

func (s *Server) bulkForm(w http.ResponseWriter, r *http.Request, status int, d *store.Device, rows []bulkRow, errKey, summary string) {
	groups, err := s.DB.ListGroups(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(rows) == 0 {
		rows = make([]bulkRow, initialBulkRows)
	}
	p := s.page(r, "bulk.title", "admin", bulkData{Device: d, Groups: groups, Rows: rows, ErrKey: errKey})
	switch {
	case errKey != "":
		p.Error = p.T("device.err." + errKey)
	case summary != "":
		p.Error = summary
	}
	s.renderStatus(w, r, status, "device_bulk", p)
}

func (s *Server) deviceBulkNew(w http.ResponseWriter, r *http.Request) {
	d := &store.Device{Kind: store.KindERS, OpenMode: store.OpenInherit, Isolation: store.IsolationSandbox,
		TLSMode: store.TLSAuto, HealthEnabled: true, MonitorSource: store.MonitorNone, MonitorDriver: store.DriverAuto,
		MonitorInterval: devices.DefaultMonitorInterval, SNMPVersion: store.SNMPv2c, SNMPPort: 161}
	s.bulkForm(w, r, http.StatusOK, d, nil, "", "")
}

// formAt returns the i-th value of a repeated form field ("" if missing).
func formAt(r *http.Request, key string, i int) string {
	v := r.PostForm[key]
	if i < len(v) {
		return v[i]
	}
	return ""
}

func (s *Server) deviceBulkCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tmpl := &store.Device{}
	readDevice(r, tmpl) // also parses the form
	tmpl.Name, tmpl.URL, tmpl.Notes = "", "", ""
	tmpl.Sort = 0
	tmpl.AutoLogin = false
	tmpl.Isolation, tmpl.TLSMode = store.IsolationSandbox, store.TLSAuto
	if tmpl.SNMPVersion == "" {
		tmpl.SNMPVersion = store.SNMPv2c
	}

	n := max(len(r.PostForm["row_name"]), len(r.PostForm["row_url"]))
	rows := make([]bulkRow, 0, n)
	for i := range n {
		rows = append(rows, bulkRow{Name: strings.TrimSpace(formAt(r, "row_name", i)), URL: strings.TrimSpace(formAt(r, "row_url", i)),
			Kind: formAt(r, "row_kind", i), Group: strings.TrimSpace(formAt(r, "row_group", i)), Num: i + 1})
	}
	used := 0
	for _, row := range rows {
		if row.Name != "" || row.URL != "" {
			used++
		}
	}
	if used == 0 {
		s.bulkForm(w, r, http.StatusUnprocessableEntity, tmpl, rows, "bulk_empty", "")
		return
	}
	if used > maxBulkRows {
		s.bulkForm(w, r, http.StatusUnprocessableEntity, tmpl, rows, "bulk_too_many", "")
		return
	}

	groups, err := s.DB.ListGroups(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	knownGroup := map[int64]bool{0: true}
	for _, g := range groups {
		knownGroup[g.ID] = true
	}
	existing, err := s.DB.ListDevices(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	seen := map[string]bool{}
	for _, d := range existing {
		seen[devices.Key(d.Name, d.URL)] = true
	}

	var devs []*store.Device
	batch := map[string]bool{}
	failed := 0
	for i := range rows {
		row := &rows[i]
		if row.Name == "" && row.URL == "" {
			continue
		}
		d := *tmpl
		d.Name, d.URL = row.Name, row.URL
		if row.Kind != "" {
			d.Kind = row.Kind
		}
		if row.Group != "" {
			gid, err := strconv.ParseInt(row.Group, 10, 64)
			if err != nil {
				gid = -1
			}
			d.GroupID = gid
		}
		errKey := ""
		if err := devices.Normalize(&d); err != nil {
			errKey = fieldKey(err)
		} else if !knownGroup[d.GroupID] {
			errKey = "invalid"
		} else if key := devices.Key(d.Name, d.URL); batch[key] {
			errKey = "duplicate"
		} else if seen[key] {
			errKey = "exists"
		}
		if errKey != "" {
			failed++
			row.Err = s.rowError(r, row.Num, errKey)
			continue
		}
		batch[devices.Key(d.Name, d.URL)] = true
		devs = append(devs, &d)
	}

	// Login and SNMP access are shared by all rows; the login only applies to
	// rows the server talks to. The login is only checked as a whole.
	anyUpstream := false
	for _, d := range devs {
		anyUpstream = anyUpstream || d.Upstream()
	}
	probe := *tmpl
	probe.URL = "http://192.0.2.1" // SNMP validation needs a host; the real ones are checked per row
	acc, accKey := s.readAccess(r, &probe, anyUpstream)
	if accKey != "" {
		s.bulkForm(w, r, http.StatusUnprocessableEntity, tmpl, rows, accKey, "")
		return
	}
	if failed > 0 {
		s.bulkForm(w, r, http.StatusUnprocessableEntity, tmpl, rows, "", s.bulkSummary(r, failed))
		return
	}

	err = s.DB.CreateDevices(ctx, devs, func(d *store.Device) error {
		if acc.WithSNMP {
			d.SNMPVersion = acc.SNMPVersion
		}
		if acc.WithLogin && d.Upstream() {
			enc, err := devices.EncryptCredentials(s.Key, d, acc.Login)
			if err != nil {
				return err
			}
			d.Credentials = enc
		}
		if acc.WithSNMP {
			enc, err := devices.EncryptSNMP(s.Key, d, acc.SNMP)
			if err != nil {
				return err
			}
			d.SNMPCredentials = enc
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, d := range devs {
		s.Audit(r, "device.create", d.Name, d.Kind+" "+d.URL)
		if d.HasCredentials() {
			s.Audit(r, "device.credentials.set", d.Name, "") // never the values
		}
		if d.HasSNMPCredentials() {
			s.Audit(r, "device.snmp.set", d.Name, "SNMP"+d.SNMPVersion) // never the values
		}
	}
	s.Audit(r, "device.bulk_create", strconv.Itoa(len(devs)), "")
	s.UpdateFrameSources(ctx)
	for _, d := range devs {
		s.deviceChanged(d.ID)
	}
	redirect(w, r, "/admin/devices?ok=bulk")
}

func (s *Server) rowError(r *http.Request, num int, key string) string {
	p := &Page{Lang: language(r)}
	return p.T("bulk.row", num) + ": " + p.T("device.err."+key)
}

func (s *Server) bulkSummary(r *http.Request, failed int) string {
	p := &Page{Lang: language(r)}
	return p.T("bulk.errors", failed)
}
