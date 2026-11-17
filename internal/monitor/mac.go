package monitor

import (
	"fmt"
	"regexp"
	"strings"
)

// FormatMAC formats six bytes as a lower-case MAC address "aa:bb:cc:dd:ee:ff".
// Any other length returns "".
func FormatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

var (
	macColon = regexp.MustCompile(`^[0-9a-fA-F]{1,2}([:-][0-9a-fA-F]{1,2}){5}$`)
	macDots  = regexp.MustCompile(`^[0-9a-fA-F]{4}\.[0-9a-fA-F]{4}\.[0-9a-fA-F]{4}$`)
	macPlain = regexp.MustCompile(`^[0-9a-fA-F]{12}$`)
)

// NormalizeMAC accepts "aa:bb:cc:dd:ee:ff", "aa-bb-cc-dd-ee-ff",
// "aabb.ccdd.eeff" and "AABBCCDDEEFF" and returns the lower-case colon form.
// It returns "" when s is not a MAC address.
func NormalizeMAC(s string) string {
	s = strings.TrimSpace(s)
	var digits string
	switch {
	case macColon.MatchString(s):
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == '-' })
		for _, p := range parts {
			if len(p) == 1 {
				p = "0" + p
			}
			digits += p
		}
	case macDots.MatchString(s):
		digits = strings.ReplaceAll(s, ".", "")
	case macPlain.MatchString(s):
		digits = s
	default:
		return ""
	}
	digits = strings.ToLower(digits)
	var sb strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(digits[i : i+2])
	}
	return sb.String()
}

func printableText(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// ChassisString formats an LLDP chassis ID. Subtype 4 (macAddress) and six
// raw bytes give the MAC format; other IDs are shown as trimmed text, or as
// hex bytes when they are not printable.
func ChassisString(subtype int, v Value) string {
	if v.Kind != KindBytes {
		return strings.TrimSpace(v.String())
	}
	b := v.Bytes
	if len(b) == 6 && (subtype == 4 || !printableText(b)) {
		return FormatMAC(b)
	}
	b = []byte(strings.TrimRight(string(b), "\x00"))
	if !printableText(b) {
		return printableID(v)
	}
	s := strings.TrimSpace(string(b))
	if subtype == 4 {
		if m := NormalizeMAC(s); m != "" {
			return m
		}
	}
	return s
}
