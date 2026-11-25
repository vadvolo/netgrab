package parser

import (
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// CambiumParser understands Cambium ePMP "config show" output: a flat
// two-column "key  value" dump (camelCase keys, values after a run of spaces,
// table sections shown as "[tableName   ]"). It reconstructs the device's
// logical interfaces from the network{Bridge,Lan,Wan} address groups and the
// wireless SSID, plus the default gateway as a route.
type CambiumParser struct{}

func (p *CambiumParser) ID() string { return "cambium-epmp" }

func (p *CambiumParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())

	kv := map[string]string{}
	for _, line := range lines(in.Raw) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue // blank or a table-section header
		}
		// key = first whitespace-delimited token; value = the rest, trimmed.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		val := strings.TrimSpace(line[len(key):])
		if _, exists := kv[key]; !exists {
			kv[key] = val
		}
	}

	if n := kv["systemConfigDeviceName"]; n != "" {
		dc.Hostname = n
	}

	// Synthesize an interface per populated address group. ePMP presents these
	// as logical roles rather than named ports.
	addIface := func(name, typ, ipKey, maskKey string) {
		ip := kv[ipKey]
		if ip == "" || ip == "0.0.0.0" {
			return
		}
		iface := &netgrabv1.Interface{Name: name, Type: typ, Enabled: true}
		if cidr := airosCIDR(ip, kv[maskKey]); cidr != "" {
			iface.IpAddresses = append(iface.IpAddresses, cidr)
			iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
		}
		dc.Interfaces = append(dc.Interfaces, iface)
	}
	addIface("bridge", "bridge", "networkBridgeIPAddr", "networkBridgeNetmask")
	addIface("lan", "ethernet", "networkLanIPAddr", "networkLanNetmask")
	addIface("wan", "ethernet", "networkWanIPAddr", "networkWanNetmask")
	addIface("mgmt", "mgmt", "mgmtIFIPAddr", "mgmtIFNetmask")

	// Wireless: the radio's own SSID, plus the AP a subscriber prefers.
	if ssid := kv["wirelessInterfaceSSID"]; ssid != "" {
		desc := "SSID " + ssid
		if up := kv["prefferedListTableEntrySSID"]; up != "" && up != ssid {
			desc += " -> " + up
		}
		dc.Interfaces = append(dc.Interfaces, &netgrabv1.Interface{
			Name: "wlan", Type: "wlan", Enabled: true, Description: desc,
		})
	}

	// Default route via the bridge gateway.
	if gw := kv["networkBridgeGatewayIP"]; gw != "" && gw != "0.0.0.0" {
		dc.Routes = append(dc.Routes, &netgrabv1.StaticRoute{Prefix: "0.0.0.0/0", NextHop: gw})
	}

	return dc, nil
}
