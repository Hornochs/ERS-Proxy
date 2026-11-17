package monitor

import "testing"

func TestNormalizeMAC(t *testing.T) {
	for in, want := range map[string]string{
		"aa:bb:cc:dd:ee:ff": "aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF": "aa:bb:cc:dd:ee:ff",
		"aabb.ccdd.eeff": "aa:bb:cc:dd:ee:ff", "AABBCCDDEEFF": "aa:bb:cc:dd:ee:ff", " 0:1b:4f:0:1:2 ": "00:1b:4f:00:01:02",
		"": "", "aa:bb:cc:dd:ee": "", "aa:bb:cc:dd:ee:gg": "", "core-sw": "", "aa:bb-cc:dd:ee:ff:00:11": "",
	} {
		if got := NormalizeMAC(in); got != want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
	if got := FormatMAC([]byte{0, 0x1B, 0x4f, 1, 2, 0xff}); got != "00:1b:4f:01:02:ff" {
		t.Errorf("FormatMAC = %q", got)
	}
	if FormatMAC([]byte{1, 2}) != "" {
		t.Error("FormatMAC of a short slice")
	}
}

func TestChassisString(t *testing.T) {
	b := func(x ...byte) Value { return Value{Kind: KindBytes, Bytes: x} }
	if got := ChassisString(4, b(2, 0, 0, 0xaa, 0, 1)); got != "02:00:00:aa:00:01" {
		t.Errorf("subtype 4: %q", got)
	}
	if got := ChassisString(7, b(2, 0, 0, 0xaa, 0, 1)); got != "02:00:00:aa:00:01" {
		t.Errorf("six raw bytes: %q", got)
	}
	if got := ChassisString(4, Value{Kind: KindBytes, Bytes: []byte("AA-BB-CC-DD-EE-FF")}); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC as text: %q", got)
	}
	if got := ChassisString(7, Value{Kind: KindBytes, Bytes: []byte(" sw-1 \x00")}); got != "sw-1" {
		t.Errorf("text: %q", got)
	}
}
