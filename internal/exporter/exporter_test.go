package exporter

import (
	"strings"
	"testing"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

func sampleSet() *netgrabv1.DeviceConfigSet {
	return &netgrabv1.DeviceConfigSet{Devices: []*netgrabv1.DeviceConfig{
		{
			Hostname: "sw-3750", Vendor: "cisco", Platform: "ios", Model: "WS-C3750-48TS",
			OsVersion: "12.2", CollectedAtUnix: 1_700_000_000, Parser: "cisco-ios",
			Interfaces: []*netgrabv1.Interface{
				{Name: "Vlan10", Type: "vlan", Enabled: true, IpAddresses: []string{"10.0.10.2/24"}, Mode: netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED},
				{Name: "FastEthernet2/0/1", Type: "ethernet", Enabled: true, Mode: netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS, AccessVlan: 10, Description: `"40.2 Printer"`},
			},
			Vlans:  []*netgrabv1.Vlan{{Id: 10, Name: "users"}},
			Routes: []*netgrabv1.StaticRoute{{Prefix: "0.0.0.0/0", NextHop: "10.0.10.1"}},
		},
		{Hostname: "nkosk", Vendor: "ros", Platform: "routeros", Model: "RB3011UiAS", Parser: "mikrotik-routeros"},
	}}
}

func TestAllFormatsExport(t *testing.T) {
	set := sampleSet()
	for _, f := range Formats() {
		e, ok := Get(f)
		if !ok {
			t.Fatalf("format %q not registered", f)
		}
		b, err := e.Export(set)
		if err != nil {
			t.Fatalf("%s export: %v", f, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s export empty", f)
		}
	}
}

func TestJSONContainsHostnames(t *testing.T) {
	e, _ := Get("json")
	b, _ := e.Export(sampleSet())
	s := string(b)
	if !strings.Contains(s, `"hostname": "sw-3750"`) || !strings.Contains(s, `"mode": "access"`) {
		t.Errorf("json missing expected content:\n%s", s)
	}
}

func TestCSVRowsPerInterface(t *testing.T) {
	e, _ := Get("csv")
	b, _ := e.Export(sampleSet())
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// header + 2 interfaces for sw-3750 + 1 empty-interface row for nkosk = 4
	if len(lines) != 4 {
		t.Errorf("expected 4 CSV lines, got %d:\n%s", len(lines), b)
	}
	if !strings.HasPrefix(lines[0], "hostname,vendor,platform") {
		t.Errorf("bad header: %s", lines[0])
	}
}

func TestTerraformStructure(t *testing.T) {
	e, _ := Get("tf")
	b, _ := e.Export(sampleSet())
	s := string(b)
	for _, want := range []string{
		`resource "netbox_manufacturer" "mfg_cisco"`,
		`resource "netbox_device_type"`,
		`resource "netbox_device" "dev_sw_3750"`,
		`resource "netbox_device_interface"`,
		`resource "netbox_ip_address"`,
		`resource "netbox_vlan" "vlan_10"`,
		`var.netbox_site_id`,
		`ip_address   = "10.0.10.2/24"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("terraform output missing %q", want)
		}
	}
}
