package connector

import "testing"

func TestResolveProfile(t *testing.T) {
	cases := []struct {
		vendor, platform string
		wantOK           bool
		wantDevice       string
		wantCmd          string
		wantSupported    bool
	}{
		{"cisco", "ios", true, "cisco", "show running-config", true},
		{"cisco", "nxos", true, "nxos", "show running-config", true},
		{"ros", "routeros", true, "ros", "/export", true},
		{"eltex", "mes", true, "eltex", "show running-config", true},
		{"huawei", "smartax", true, "huawei", "display saved-configuration", true},
		{"ubiquiti", "airos", true, "pc", "cat /tmp/system.cfg", true},
		// platform empty -> vendor fallback.
		{"huawei", "", true, "huawei", "display current-configuration", true},
		// unsupported classes resolve but are marked not collectable.
		{"apc", "apc", true, "", "", false},
		{"microsoft", "windows", true, "", "", false},
		// completely unknown -> not resolved.
		{"acme", "widgetos", false, "", "", false},
		// case/space insensitivity.
		{"  Cisco ", " IOS ", true, "cisco", "show running-config", true},
	}
	for _, c := range cases {
		got, ok := ResolveProfile(c.vendor, c.platform)
		if ok != c.wantOK {
			t.Errorf("ResolveProfile(%q,%q) ok=%v want %v", c.vendor, c.platform, ok, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if got.GnetcliDevice != c.wantDevice || got.Command != c.wantCmd || got.Supported != c.wantSupported {
			t.Errorf("ResolveProfile(%q,%q) = {dev:%q cmd:%q supported:%v} want {dev:%q cmd:%q supported:%v}",
				c.vendor, c.platform, got.GnetcliDevice, got.Command, got.Supported,
				c.wantDevice, c.wantCmd, c.wantSupported)
		}
	}
}
