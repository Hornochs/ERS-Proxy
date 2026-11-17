package devices

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/secret"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

func TestNormalizeSSH(t *testing.T) {
	var fe *FieldError
	for _, c := range []struct {
		d   store.Device
		key string // "" = valid
	}{
		{store.Device{Kind: store.KindERS, URL: "http://192.0.2.1", SSHEnabled: true, SSHProfile: "legacy"}, ""},
		{store.Device{Kind: store.KindVSP, URL: "https://192.0.2.2", SSHEnabled: true, SSHProfile: "legacy"}, "ssh_profile"},
		{store.Device{Kind: store.KindVSP, URL: "https://192.0.2.2", SSHEnabled: true, SSHProfile: "compat"}, ""},
		{store.Device{Kind: store.KindRouterOS, URL: "https://192.0.2.3", SSHEnabled: true, SSHLogin: "web"}, ""},
		{store.Device{Kind: store.KindLink, URL: "https://192.0.2.4/", SSHEnabled: true, SSHLogin: "web"}, "ssh_login"},
		{store.Device{Kind: store.KindLink, URL: "https://192.0.2.4/", SSHEnabled: true, SSHLogin: "ask"}, ""},
		{store.Device{Kind: store.KindSwOS, URL: "http://192.0.2.5", SSHEnabled: true}, "ssh_enabled"},
		{store.Device{Kind: store.KindERS, URL: "http://192.0.2.1", SSHPort: 70000}, "ssh_port"},
		{store.Device{Kind: store.KindERS, URL: "http://192.0.2.1", SSHLogin: "other"}, "ssh_login"},
		{store.Device{Kind: store.KindERS, URL: "http://192.0.2.1", SSHProfile: "other"}, "ssh_profile"},
	} {
		d := c.d
		err := NormalizeSSH(&d)
		switch {
		case c.key == "" && err != nil:
			t.Errorf("%s %+v: %v", d.Kind, c.d, err)
		case c.key != "" && (!errors.As(err, &fe) || fe.Key != c.key):
			t.Errorf("%s %+v: want %s error, got %v", d.Kind, c.d, c.key, err)
		}
		if c.key == "" && (d.SSHPort != 22 || d.SSHProfile == "") {
			t.Errorf("defaults not filled: %+v", d)
		}
	}
}

func TestSSHLoginDefaults(t *testing.T) {
	for kind, want := range map[string]string{store.KindERS: "web", store.KindRouterOS: "web", store.KindVSP: "own",
		store.KindEDMDirect: "own", store.KindLink: "ask"} {
		if got := SSHLogin(&store.Device{Kind: kind}); got != want {
			t.Errorf("%s: %s, want %s", kind, got, want)
		}
	}
	if got := SSHLogin(&store.Device{Kind: store.KindERS, SSHLogin: "ask"}); got != "ask" {
		t.Errorf("explicit source ignored: %s", got)
	}
	if SSHAddr(&store.Device{URL: "https://[2001:db8::1]:8443", SSHPort: 2222}) != "[2001:db8::1]:2222" {
		t.Error("SSHAddr")
	}
}

func TestSSHCredentials(t *testing.T) {
	k, err := secret.LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	if err != nil {
		t.Fatal(err)
	}
	d := &store.Device{ID: 7, Kind: store.KindVSP, URL: "https://192.0.2.9", SSHPort: 22}
	if _, ok, err := SSHCredentials(k, d); ok || err != nil {
		t.Fatalf("nothing stored: %v %v", ok, err)
	}
	enc, err := EncryptSSH(k, d, Credentials{Username: "rwa", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	d.SSHCredentials = enc
	c, ok, err := SSHCredentials(k, d)
	if !ok || err != nil || c.Username != "rwa" || c.Password != "pw" {
		t.Fatalf("own login: %+v %v %v", c, ok, err)
	}
	// Bound to the SSH port and the address.
	moved := *d
	moved.SSHPort = 2222
	if _, _, err := SSHCredentials(k, &moved); err == nil {
		t.Fatal("SSH login opened for another port")
	}
	// The web login is used for ERS.
	ers := &store.Device{ID: 8, Kind: store.KindERS, URL: "http://192.0.2.10"}
	web, err := EncryptCredentials(k, ers, Credentials{Username: "RW", Password: "secure"})
	if err != nil {
		t.Fatal(err)
	}
	ers.Credentials = web
	if c, ok, err := SSHCredentials(k, ers); !ok || err != nil || c.Username != "RW" {
		t.Fatalf("web login: %+v %v %v", c, ok, err)
	}
	ers.SSHLogin = SSHLoginAsk
	if _, ok, _ := SSHCredentials(k, ers); ok {
		t.Fatal("ask must not use a stored login")
	}
	if _, err := EncryptSSH(k, &store.Device{URL: "http://x"}, Credentials{}); err == nil {
		t.Fatal("encrypted for an unsaved device")
	}
}
