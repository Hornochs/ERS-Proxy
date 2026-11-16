package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/lan-dot-party/ers-proxy/internal/secret"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// SSH console login sources (store.Device.SSHLogin).
const (
	SSHLoginWeb = "web" // the device's stored web login (ERS, RouterOS)
	SSHLoginOwn = "own" // an SSH login stored for the console (VOSS)
	SSHLoginAsk = "ask" // typed in the console every time
)

// SSH algorithm profiles (store.Device.SSHProfile), see internal/webssh.
const (
	SSHProfileAuto   = "auto"
	SSHProfileModern = "modern"
	SSHProfileLegacy = "legacy"
)

// SSHSupported reports whether the SSH console can be offered for d: every
// kind except SwOS, which has no SSH server.
func SSHSupported(d *store.Device) bool { return d.Kind != store.KindSwOS }

// SSHLegacyAllowed reports whether the legacy algorithm profile (the frozen
// SSH copy with ssh-dss, SHA-1 key exchange and HMAC-MD5) may be used for d:
// only for ERS switches.
func SSHLegacyAllowed(d *store.Device) bool { return d.Kind == store.KindERS }

// DefaultSSHLogin is the login source of a kind when none is set: ERS and
// RouterOS accept their web login over SSH, VOSS has separate accounts.
func DefaultSSHLogin(kind string) string {
	switch kind {
	case store.KindERS, store.KindRouterOS:
		return SSHLoginWeb
	case store.KindVSP, store.KindEDMDirect:
		return SSHLoginOwn
	}
	return SSHLoginAsk
}

// SSHLogin returns the effective login source of d.
func SSHLogin(d *store.Device) string {
	if d.SSHLogin == "" {
		return DefaultSSHLogin(d.Kind)
	}
	return d.SSHLogin
}

// NormalizeSSH validates the SSH console settings of d.
func NormalizeSSH(d *store.Device) error {
	if d.SSHPort == 0 {
		d.SSHPort = 22
	}
	if d.SSHPort < 1 || d.SSHPort > 65535 {
		return fieldErr("ssh_port", "SSH port %d: want 1–65535", d.SSHPort)
	}
	switch d.SSHLogin {
	case "", SSHLoginWeb, SSHLoginOwn, SSHLoginAsk:
	default:
		return fieldErr("ssh_login", "SSH login %q: want web, own or ask", d.SSHLogin)
	}
	if d.SSHLogin == SSHLoginWeb && !d.Upstream() {
		return fieldErr("ssh_login", "a %s device has no stored web login", d.Kind)
	}
	switch d.SSHProfile {
	case "":
		d.SSHProfile = SSHProfileAuto
	case SSHProfileAuto, SSHProfileModern:
	case SSHProfileLegacy:
		if !SSHLegacyAllowed(d) {
			return fieldErr("ssh_profile", "the legacy SSH profile is only available for ERS switches")
		}
	default:
		return fieldErr("ssh_profile", "SSH profile %q: want auto, modern or legacy", d.SSHProfile)
	}
	if d.SSHEnabled && !SSHSupported(d) {
		return fieldErr("ssh_enabled", "%s devices have no SSH console", d.Kind)
	}
	return nil
}

// SSHAddr is host:port of the device's SSH server.
func SSHAddr(d *store.Device) string {
	host, _ := HostPort(d.URL)
	return net.JoinHostPort(host, strconv.Itoa(d.SSHPort))
}

// SSHAAD is the associated data that binds a stored SSH login to a device,
// its host and its SSH port.
func SSHAAD(d *store.Device) []byte {
	host, _ := HostPort(d.URL)
	return []byte("ers-proxy ssh|" + fmt.Sprint(d.ID) + "|" + host + "|" + fmt.Sprint(d.SSHPort))
}

// EncryptSSH encrypts an SSH login for d. d.ID must be set.
func EncryptSSH(k *secret.Key, d *store.Device, c Credentials) ([]byte, error) {
	if d.ID == 0 {
		return nil, errors.New("device must be saved before an SSH login is stored")
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return k.Encrypt(plain, SSHAAD(d))
}

// DecryptSSH opens the SSH login stored for d.
func DecryptSSH(k *secret.Key, d *store.Device) (Credentials, error) {
	var c Credentials
	if !d.HasSSHCredentials() {
		return c, errors.New("no SSH login stored")
	}
	plain, err := k.Decrypt(d.SSHCredentials, SSHAAD(d))
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(plain, &c)
	return c, err
}

// SSHCredentials returns the stored login the console uses for d, and false
// when the user has to type one (source ask, or nothing stored).
func SSHCredentials(k *secret.Key, d *store.Device) (Credentials, bool, error) {
	switch SSHLogin(d) {
	case SSHLoginWeb:
		if !d.HasCredentials() {
			return Credentials{}, false, nil
		}
		c, err := DecryptCredentials(k, d)
		return c, err == nil, err
	case SSHLoginOwn:
		if !d.HasSSHCredentials() {
			return Credentials{}, false, nil
		}
		c, err := DecryptSSH(k, d)
		return c, err == nil, err
	}
	return Credentials{}, false, nil
}
