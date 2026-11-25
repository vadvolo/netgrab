package parser

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// AirOSParser understands Ubiquiti airOS "system.cfg" (collected via
// `cat /tmp/system.cfg`), the second-largest platform in the Orion fleet
// (~1.6k CPE/AP radios). system.cfg is a flat dotted-key = value file:
//
//	resolv.host.1.name=AC001-S2
//	netconf.3.devname=br0
//	netconf.3.ip=172.28.61.122
//	netconf.3.netmask=255.255.255.248
//	route.1.ip=0.0.0.0 / route.1.netmask=0 / route.1.gateway=172.28.61.121
//	wireless.1.ssid=orionwifi-... / radio.1.mode=master
type AirOSParser struct{}

func (p *AirOSParser) ID() string { return "ubiquiti-airos" }

func (p *AirOSParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())

	kv := map[string]string{}
	for _, line := range lines(in.Raw) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			kv[line[:eq]] = line[eq+1:]
		}
	}

	if h := kv["resolv.host.1.name"]; h != "" {
		dc.Hostname = h
	}

	// Interfaces: one per netconf.<idx> group.
	for _, idx := range indicesFor(kv, "netconf.") {
		pfx := "netconf." + idx + "."
		name := kv[pfx+"devname"]
		if name == "" {
			continue
		}
		iface := &netgrabv1.Interface{
			Name:    name,
			Type:    interfaceType(name),
			Enabled: kv[pfx+"status"] != "disabled" && kv[pfx+"up"] != "disabled",
		}
		if mtu, err := strconv.Atoi(kv[pfx+"mtu"]); err == nil {
			iface.Mtu = uint32(mtu)
		}
		if ip := kv[pfx+"ip"]; ip != "" {
			if cidr := airosCIDR(ip, kv[pfx+"netmask"]); cidr != "" {
				iface.IpAddresses = append(iface.IpAddresses, cidr)
				iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
			}
		}
		dc.Interfaces = append(dc.Interfaces, iface)
	}

	// Attach the SSID / radio role to the wireless interface as a description,
	// so the single most useful radio fact survives into the model.
	ssid := firstNonEmpty(kv["wireless.1.ssid"], kv["radio.1.ssid"])
	if ssid != "" {
		mode := kv["radio.1.mode"] // master (AP) / managed (station)
		desc := "SSID " + ssid
		if mode != "" {
			desc += " (" + mode + ")"
		}
		for _, iface := range dc.Interfaces {
			if iface.Type == "wlan" && iface.Description == "" {
				iface.Description = desc
			}
		}
	}

	// Routes: one per route.<idx> group with a gateway.
	for _, idx := range indicesFor(kv, "route.") {
		pfx := "route." + idx + "."
		ip, gw := kv[pfx+"ip"], kv[pfx+"gateway"]
		if ip == "" || gw == "" || kv[pfx+"status"] == "disabled" {
			continue
		}
		cidr := airosCIDR(ip, kv[pfx+"netmask"])
		if cidr == "" {
			continue
		}
		dc.Routes = append(dc.Routes, &netgrabv1.StaticRoute{Prefix: cidr, NextHop: gw})
	}

	return dc, nil
}

// airosCIDR builds a CIDR from an airOS ip + netmask, where netmask is either a
// dotted mask ("255.255.255.248") or a bare prefix length ("0", "24").
func airosCIDR(ip, mask string) string {
	if ip == "" {
		return ""
	}
	if mask == "" {
		return ip
	}
	if strings.Contains(mask, ".") {
		return maskToCIDR(ip, mask)
	}
	n, err := strconv.Atoi(mask)
	if err != nil || n < 0 || n > 32 {
		return ""
	}
	if net.ParseIP(ip) == nil {
		return ""
	}
	return fmt.Sprintf("%s/%d", ip, n)
}

// indicesFor returns the sorted distinct numeric indices that appear directly
// after prefix, e.g. prefix "netconf." over "netconf.3.ip" yields "3".
func indicesFor(kv map[string]string, prefix string) []string {
	seen := map[string]bool{}
	for k := range kv {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		dot := strings.IndexByte(rest, '.')
		if dot <= 0 {
			continue
		}
		idx := rest[:dot]
		if _, err := strconv.Atoi(idx); err == nil {
			seen[idx] = true
		}
	}
	out := make([]string, 0, len(seen))
	for idx := range seen {
		out = append(out, idx)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i])
		b, _ := strconv.Atoi(out[j])
		return a < b
	})
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
