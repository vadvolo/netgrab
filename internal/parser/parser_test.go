package parser

import (
	"os"
	"testing"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		vendor, platform, want string
	}{
		{"ros", "routeros", "mikrotik-routeros"},
		{"cisco", "ios", "cisco-ios"},
		{"eltex", "mes", "cisco-ios"},
		{"ubiquiti", "airos", "ubiquiti-airos"},
		{"huawei", "smartax", "huawei-vrp"},
		{"cambium", "epmp", "cambium-epmp"},
		{"", "", "generic"},
	}
	for _, c := range cases {
		if got := Resolve(c.vendor, c.platform).ID(); got != c.want {
			t.Errorf("Resolve(%q,%q)=%q want %q", c.vendor, c.platform, got, c.want)
		}
	}
}

func TestCiscoParseRealConfig(t *testing.T) {
	raw, err := os.ReadFile("../../tmp/local-storage/sw-3750/sw-3750_config_2025-06-11.txt")
	if err != nil {
		t.Skipf("sample config not present: %v", err)
	}
	dc, err := Parse(Input{Hostname: "sw-3750", Vendor: "cisco", Platform: "ios", SourceCommand: "show running-config", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Hostname != "sw-3750" {
		t.Errorf("hostname=%q", dc.Hostname)
	}
	if dc.OsVersion != "12.2" {
		t.Errorf("os_version=%q want 12.2", dc.OsVersion)
	}
	if dc.RawConfigSha256 == "" || dc.RawConfigBytes == 0 {
		t.Error("provenance not populated")
	}
	// Interfaces: Loopback0 routed with /32, trunk Port-channel1, access ports.
	var lo0, po1, fa *netgrabv1.Interface
	for _, i := range dc.Interfaces {
		switch i.Name {
		case "Loopback0":
			lo0 = i
		case "Port-channel1":
			po1 = i
		case "FastEthernet2/0/1":
			fa = i
		}
	}
	if lo0 == nil || len(lo0.IpAddresses) != 1 || lo0.IpAddresses[0] != "10.10.0.1/32" {
		t.Errorf("Loopback0 addr wrong: %+v", lo0)
	}
	if po1 == nil || po1.Mode != netgrabv1.InterfaceMode_INTERFACE_MODE_TRUNK {
		t.Errorf("Port-channel1 mode wrong: %+v", po1)
	}
	if fa == nil || fa.Mode != netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS || fa.AccessVlan != 10 {
		t.Errorf("FastEthernet2/0/1 wrong: %+v", fa)
	}
	// Default route.
	foundDefault := false
	for _, r := range dc.Routes {
		if r.Prefix == "0.0.0.0/0" && r.NextHop == "10.0.10.1" {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Errorf("default route not parsed: %+v", dc.Routes)
	}
	t.Logf("parsed %d interfaces, %d vlans, %d routes", len(dc.Interfaces), len(dc.Vlans), len(dc.Routes))
}

func TestHuaweiParse(t *testing.T) {
	raw := `!Software Version V100R018
[MA5800V100R018: 8300]
#
sysname STAV-XPON-MA5800-X17
#
 vlan 2 smart
 vlan 70 to 74 smart
 vlan batch 100 200
#
interface Vlanif2
 description "uplink"
 ip address 192.168.152.162 255.255.255.248
#
interface Vlanif132
 ip address 172.24.150.146 255.255.255.252
 shutdown
#
ip route-static 0.0.0.0 0.0.0.0 172.24.150.145 description "GATEWAY"
ip route-static vpn-instance "TELEKOM" 0.0.0.0 0.0.0.0 192.168.152.161
#
`
	dc, err := Parse(Input{Vendor: "huawei", Platform: "smartax", SourceCommand: "display saved-configuration", Raw: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Parser != "huawei-vrp" {
		t.Errorf("parser=%q", dc.Parser)
	}
	if dc.Hostname != "STAV-XPON-MA5800-X17" {
		t.Errorf("hostname=%q", dc.Hostname)
	}
	if dc.Model != "MA5800" || dc.OsVersion != "V100R018" {
		t.Errorf("model/os=%q/%q", dc.Model, dc.OsVersion)
	}
	// 2, 70-74 (5), 100, 200 = 8 vlans
	if len(dc.Vlans) != 8 {
		t.Errorf("vlans=%d want 8: %+v", len(dc.Vlans), dc.Vlans)
	}
	var vlanif2, vlanif132 *netgrabv1.Interface
	for _, i := range dc.Interfaces {
		switch i.Name {
		case "Vlanif2":
			vlanif2 = i
		case "Vlanif132":
			vlanif132 = i
		}
	}
	if vlanif2 == nil || len(vlanif2.IpAddresses) != 1 || vlanif2.IpAddresses[0] != "192.168.152.162/29" {
		t.Errorf("Vlanif2 wrong: %+v", vlanif2)
	}
	if vlanif132 == nil || vlanif132.Enabled {
		t.Errorf("Vlanif132 should be shut down: %+v", vlanif132)
	}
	// Only the global default route; the vpn-instance one is skipped.
	if len(dc.Routes) != 1 || dc.Routes[0].Prefix != "0.0.0.0/0" || dc.Routes[0].NextHop != "172.24.150.145" {
		t.Errorf("routes wrong: %+v", dc.Routes)
	}
}

func TestAirOSParse(t *testing.T) {
	raw := `aaa.status=disabled
bridge.1.devname=br0
netconf.1.devname=ath0
netconf.1.status=enabled
netconf.1.up=enabled
netconf.1.mtu=2024
netconf.2.devname=eth0
netconf.2.status=disabled
netconf.3.devname=br0
netconf.3.ip=172.28.61.122
netconf.3.netmask=255.255.255.248
netconf.3.status=enabled
radio.1.mode=master
resolv.host.1.name=AC001-S2
route.1.devname=br0
route.1.gateway=172.28.61.121
route.1.ip=0.0.0.0
route.1.netmask=0
route.1.status=enabled
wireless.1.ssid=orionwifi-test
`
	dc, err := Parse(Input{Vendor: "ubiquiti", Platform: "airos", SourceCommand: "cat /tmp/system.cfg", Raw: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Parser != "ubiquiti-airos" {
		t.Errorf("parser=%q", dc.Parser)
	}
	if dc.Hostname != "AC001-S2" {
		t.Errorf("hostname=%q", dc.Hostname)
	}
	byName := map[string]*netgrabv1.Interface{}
	for _, i := range dc.Interfaces {
		byName[i.Name] = i
	}
	if a := byName["ath0"]; a == nil || a.Type != "wlan" || !a.Enabled || a.Description != "SSID orionwifi-test (master)" {
		t.Errorf("ath0 wrong: %+v", a)
	}
	if e := byName["eth0"]; e == nil || e.Enabled {
		t.Errorf("eth0 should be disabled: %+v", e)
	}
	if b := byName["br0"]; b == nil || len(b.IpAddresses) != 1 || b.IpAddresses[0] != "172.28.61.122/29" {
		t.Errorf("br0 wrong: %+v", b)
	}
	if len(dc.Routes) != 1 || dc.Routes[0].Prefix != "0.0.0.0/0" || dc.Routes[0].NextHop != "172.28.61.121" {
		t.Errorf("routes wrong: %+v", dc.Routes)
	}
}

func TestCambiumParse(t *testing.T) {
	raw := `acsEnable                                0
[cambiumStaticRoutesCnfTable             ]
networkBridgeGatewayIP                   10.128.39.145
networkBridgeIPAddr                      10.128.39.147
networkBridgeNetmask                     255.255.255.248
networkLanIPAddr                         10.1.1.254
networkLanNetmask                        255.255.255.0
networkWanIPAddr                         0.0.0.0
networkWanNetmask                        255.255.255.0
        prefferedListTableEntrySSID               or-torlak-gaslider
systemConfigDeviceName                   Station_Cli:Gas Lider Vrcin
wirelessInterfaceSSID                    Cambium-F300-25-AP
`
	dc, err := Parse(Input{Vendor: "cambium", Platform: "epmp", SourceCommand: "config show", Raw: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Parser != "cambium-epmp" {
		t.Errorf("parser=%q", dc.Parser)
	}
	if dc.Hostname != "Station_Cli:Gas Lider Vrcin" {
		t.Errorf("hostname=%q", dc.Hostname)
	}
	byName := map[string]*netgrabv1.Interface{}
	for _, i := range dc.Interfaces {
		byName[i.Name] = i
	}
	if b := byName["bridge"]; b == nil || len(b.IpAddresses) != 1 || b.IpAddresses[0] != "10.128.39.147/29" {
		t.Errorf("bridge wrong: %+v", b)
	}
	if _, ok := byName["wan"]; ok {
		t.Errorf("wan has 0.0.0.0 and should be skipped")
	}
	if w := byName["wlan"]; w == nil || w.Description != "SSID Cambium-F300-25-AP -> or-torlak-gaslider" {
		t.Errorf("wlan wrong: %+v", w)
	}
	if len(dc.Routes) != 1 || dc.Routes[0].NextHop != "10.128.39.145" {
		t.Errorf("routes wrong: %+v", dc.Routes)
	}
}

func TestRouterOSParse(t *testing.T) {
	raw := `# oct/22/2026 10:00:00 by RouterOS 6.49.10
# model = RB3011UiAS
# serial number = ABC123
/interface bridge
add name=bridge1 comment="LAN bridge"
/interface ethernet
set [ find default-name=ether1 ] comment="WAN uplink" name=ether1
set [ find default-name=ether2 ] name=ether2 disabled=yes
/interface vlan
add interface=bridge1 name=vlan100 vlan-id=100
/ip address
add address=10.0.0.1/24 interface=bridge1 network=10.0.0.0
add address=192.168.88.1/24 interface=ether1
/ip route
add distance=1 dst-address=0.0.0.0/0 gateway=10.0.0.254
/system identity
set name=nkosk
`
	dc, err := Parse(Input{Vendor: "ros", Platform: "routeros", SourceCommand: "/export", Raw: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Hostname != "nkosk" {
		t.Errorf("hostname=%q want nkosk", dc.Hostname)
	}
	if dc.Model != "RB3011UiAS" {
		t.Errorf("model=%q", dc.Model)
	}
	if dc.OsVersion != "6.49.10" {
		t.Errorf("os_version=%q", dc.OsVersion)
	}
	byName := map[string]*netgrabv1.Interface{}
	for _, i := range dc.Interfaces {
		byName[i.Name] = i
	}
	if b := byName["bridge1"]; b == nil || len(b.IpAddresses) != 1 || b.IpAddresses[0] != "10.0.0.1/24" {
		t.Errorf("bridge1 wrong: %+v", b)
	}
	if e := byName["ether1"]; e == nil || e.Description != "WAN uplink" {
		t.Errorf("ether1 desc wrong: %+v", e)
	}
	if e := byName["ether2"]; e == nil || e.Enabled {
		t.Errorf("ether2 should be disabled: %+v", e)
	}
	if v := byName["vlan100"]; v == nil || v.AccessVlan != 100 {
		t.Errorf("vlan100 wrong: %+v", v)
	}
	if len(dc.Vlans) != 1 || dc.Vlans[0].Id != 100 {
		t.Errorf("vlans wrong: %+v", dc.Vlans)
	}
	if len(dc.Routes) != 1 || dc.Routes[0].Prefix != "0.0.0.0/0" || dc.Routes[0].NextHop != "10.0.0.254" {
		t.Errorf("routes wrong: %+v", dc.Routes)
	}
}
