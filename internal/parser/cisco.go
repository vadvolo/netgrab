package parser

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// CiscoParser understands IOS / IOS-XE / IOS-XR / NX-OS "show running-config"
// output and the many IOS-like grammars in the Orion fleet (Eltex MES, Ruijie,
// HP ProCurve, Zyxel, TP-Link, DCN). They share the "global lines + indented
// interface stanzas" shape this parser keys off.
type CiscoParser struct{}

func (p *CiscoParser) ID() string { return "cisco-ios" }

func (p *CiscoParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())
	ls := lines(in.Raw)

	// vlanNames collects explicit "vlan N / name X" stanzas.
	vlanNames := map[uint32]string{}

	for i := 0; i < len(ls); i++ {
		line := ls[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "hostname "):
			dc.Hostname = unquote(strings.TrimSpace(trimmed[len("hostname "):]))

		case strings.HasPrefix(trimmed, "version ") && dc.OsVersion == "":
			dc.OsVersion = strings.TrimSpace(trimmed[len("version "):])

		case strings.HasPrefix(trimmed, "switch ") && strings.Contains(trimmed, "provision "):
			// e.g. "switch 2 provision ws-c3750-48ts"
			if idx := strings.Index(trimmed, "provision "); idx >= 0 {
				dc.Model = strings.ToUpper(strings.TrimSpace(trimmed[idx+len("provision "):]))
			}

		case strings.HasPrefix(trimmed, "ip route "):
			if r := parseCiscoRoute(trimmed); r != nil {
				dc.Routes = append(dc.Routes, r)
			}

		case isGlobalLine(line) && strings.HasPrefix(trimmed, "vlan ") && !strings.Contains(trimmed, "internal") && !strings.Contains(trimmed, "allocation"):
			// "vlan 10" (possibly "vlan 10,20,30") optionally followed by "name X".
			ids := parseVlanList(strings.TrimSpace(trimmed[len("vlan "):]))
			// Look ahead for an indented "name" line (single-id stanzas only).
			name := ""
			if len(ids) == 1 && i+1 < len(ls) && strings.HasPrefix(ls[i+1], " ") {
				nt := strings.TrimSpace(ls[i+1])
				if strings.HasPrefix(nt, "name ") {
					name = unquote(strings.TrimSpace(nt[len("name "):]))
				}
			}
			for _, id := range ids {
				if name != "" {
					vlanNames[id] = name
				} else if _, ok := vlanNames[id]; !ok {
					vlanNames[id] = ""
				}
			}

		case isGlobalLine(line) && strings.HasPrefix(trimmed, "interface "):
			iface, consumed := parseCiscoInterface(ls, i)
			if iface != nil {
				dc.Interfaces = append(dc.Interfaces, iface)
			}
			i += consumed
		}
	}

	for id, name := range vlanNames {
		dc.Vlans = append(dc.Vlans, &netgrabv1.Vlan{Id: id, Name: name})
	}
	sortVlans(dc.Vlans)
	return dc, nil
}

// parseCiscoInterface reads the interface stanza starting at ls[start] and all
// its indented sub-lines. It returns the interface and how many extra lines
// (beyond the "interface" line) it consumed.
func parseCiscoInterface(ls []string, start int) (*netgrabv1.Interface, int) {
	name := strings.TrimSpace(strings.TrimSpace(ls[start])[len("interface "):])
	if name == "" {
		return nil, 0
	}
	iface := &netgrabv1.Interface{
		Name:    name,
		Type:    interfaceType(name),
		Enabled: true, // IOS interfaces are up unless "shutdown" appears
		Mode:    netgrabv1.InterfaceMode_INTERFACE_MODE_UNSPECIFIED,
	}

	consumed := 0
	for j := start + 1; j < len(ls); j++ {
		line := ls[j]
		if !isIndented(line) {
			break // stanza ends at the next global line (including "!")
		}
		consumed++
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "description "):
			iface.Description = unquote(strings.TrimSpace(t[len("description "):]))
		case t == "shutdown":
			iface.Enabled = false
		case t == "no shutdown":
			iface.Enabled = true
		case strings.HasPrefix(t, "mtu "):
			if v, err := strconv.Atoi(strings.Fields(t)[1]); err == nil {
				iface.Mtu = uint32(v)
			}
		case strings.HasPrefix(t, "ip address ") && !strings.HasPrefix(t, "ip address dhcp"):
			if cidr := parseIPMask(strings.TrimSpace(t[len("ip address "):])); cidr != "" {
				iface.IpAddresses = append(iface.IpAddresses, cidr)
			}
		case strings.HasPrefix(t, "ipv6 address "):
			if a := strings.Fields(t[len("ipv6 address "):]); len(a) > 0 && strings.Contains(a[0], "/") {
				iface.IpAddresses = append(iface.IpAddresses, a[0])
			}
		case t == "no switchport":
			iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
		case t == "switchport mode access":
			iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS
		case t == "switchport mode trunk":
			iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_TRUNK
		case strings.HasPrefix(t, "switchport access vlan "):
			if v, err := strconv.Atoi(strings.Fields(t)[3]); err == nil {
				iface.AccessVlan = uint32(v)
			}
		case strings.HasPrefix(t, "switchport trunk allowed vlan add "):
			iface.TaggedVlans = append(iface.TaggedVlans, parseVlanList(strings.TrimSpace(t[len("switchport trunk allowed vlan add "):]))...)
		case strings.HasPrefix(t, "switchport trunk allowed vlan "):
			iface.TaggedVlans = parseVlanList(strings.TrimSpace(t[len("switchport trunk allowed vlan "):]))
		}
	}
	// A routed interface with addresses but no explicit "no switchport".
	if iface.Mode == netgrabv1.InterfaceMode_INTERFACE_MODE_UNSPECIFIED && len(iface.IpAddresses) > 0 {
		iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
	}
	return iface, consumed
}

// parseCiscoRoute parses "ip route PREFIX MASK NEXTHOP [distance]".
func parseCiscoRoute(line string) *netgrabv1.StaticRoute {
	f := strings.Fields(line)
	// ip route <prefix> <mask> <nexthop> [dist]
	if len(f) < 5 {
		return nil
	}
	cidr := maskToCIDR(f[2], f[3])
	if cidr == "" {
		return nil
	}
	r := &netgrabv1.StaticRoute{Prefix: cidr, NextHop: f[4]}
	if len(f) >= 6 {
		if d, err := strconv.Atoi(f[5]); err == nil {
			r.Distance = uint32(d)
		}
	}
	return r
}

// --- shared helpers ---

// isIndented reports whether a config line belongs to the current stanza
// (leading whitespace). "!" and column-0 keywords end the stanza.
func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

// isGlobalLine is the inverse: a line that starts a global/top-level statement.
func isGlobalLine(line string) bool {
	return line != "" && !isIndented(line)
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseIPMask turns "10.0.0.1 255.255.255.0" or "10.0.0.1/24" into CIDR.
func parseIPMask(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	if strings.Contains(f[0], "/") {
		return f[0]
	}
	if len(f) >= 2 {
		return maskToCIDR(f[0], f[1])
	}
	return ""
}

// maskToCIDR converts "10.0.0.0 255.255.255.0" into "10.0.0.0/24".
func maskToCIDR(ip, mask string) string {
	addr := net.ParseIP(ip)
	m := net.ParseIP(mask)
	if addr == nil || m == nil {
		return ""
	}
	v4 := m.To4()
	if v4 == nil {
		return ""
	}
	ones, _ := net.IPMask(v4).Size()
	return fmt.Sprintf("%s/%d", ip, ones)
}

// parseVlanList parses "10", "10,20,30", "10-12", "10,20-22" into ids.
func parseVlanList(s string) []uint32 {
	var out []uint32
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(r[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(r[1]))
			if err1 == nil && err2 == nil && lo <= hi {
				for v := lo; v <= hi; v++ {
					out = append(out, uint32(v))
				}
			}
			continue
		}
		if v, err := strconv.Atoi(part); err == nil {
			out = append(out, uint32(v))
		}
	}
	return out
}

// interfaceType classifies an interface by name prefix.
func interfaceType(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(n, "loopback") || strings.HasPrefix(n, "lo"):
		return "loopback"
	case strings.HasPrefix(n, "vlan") || strings.HasPrefix(n, "bvi"):
		return "vlan"
	case strings.HasPrefix(n, "port-channel") || strings.HasPrefix(n, "po") || strings.HasPrefix(n, "bundle") || strings.HasPrefix(n, "bond"):
		return "lag"
	case strings.HasPrefix(n, "tunnel"):
		return "tunnel"
	case strings.HasPrefix(n, "bridge") || strings.HasPrefix(n, "br"):
		return "bridge"
	case strings.HasPrefix(n, "wlan") || strings.HasPrefix(n, "wifi") || strings.HasPrefix(n, "dot11") || strings.HasPrefix(n, "ath"):
		return "wlan"
	case strings.HasPrefix(n, "mgmt") || strings.HasPrefix(n, "management"):
		return "mgmt"
	default:
		return "ethernet"
	}
}

func sortVlans(v []*netgrabv1.Vlan) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j-1].Id > v[j].Id; j-- {
			v[j-1], v[j] = v[j], v[j-1]
		}
	}
}
