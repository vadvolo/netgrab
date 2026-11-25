package parser

import (
	"regexp"
	"strconv"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// HuaweiParser understands Huawei VRP output — both "display current-configuration"
// on VRP switches and "display saved-configuration" on SmartAX OLTs (MA5800).
// VRP shares the "global lines + indented interface stanzas" shape with IOS but
// uses sysname, "vlan N smart" / "vlan N to M" definitions, "port default vlan"
// switching, and "ip route-static" routes.
type HuaweiParser struct{}

func (p *HuaweiParser) ID() string { return "huawei-vrp" }

// huaweiVersion matches the saved-config banner, e.g. "[MA5800V100R018: 8300]".
var huaweiVersion = regexp.MustCompile(`\[([A-Z0-9]+)(V\d+R\d+[A-Z0-9]*)[^\]]*\]`)

func (p *HuaweiParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())
	ls := lines(in.Raw)

	for i := 0; i < len(ls); i++ {
		line := ls[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "sysname "):
			dc.Hostname = unquote(strings.TrimSpace(trimmed[len("sysname "):]))

		case dc.Model == "" && strings.Contains(trimmed, "["):
			if m := huaweiVersion.FindStringSubmatch(trimmed); m != nil {
				dc.Model = m[1]
				dc.OsVersion = m[2]
			}

		case strings.HasPrefix(trimmed, "vlan "):
			// VLAN definitions ("vlan N smart", "vlan N to M", "vlan batch ...")
			// may be indented in OLT saved-configuration, so don't require a
			// column-0 line here. Interface sub-config never starts with "vlan"
			// (it uses "port ... vlan"), so this won't catch those.
			for _, id := range parseHuaweiVlans(strings.TrimSpace(trimmed[len("vlan "):])) {
				addVlan(dc, id, "")
			}

		case strings.HasPrefix(trimmed, "ip route-static "):
			if r := parseHuaweiRoute(trimmed); r != nil {
				dc.Routes = append(dc.Routes, r)
			}

		case isGlobalLine(line) && strings.HasPrefix(trimmed, "interface "):
			iface, consumed := parseHuaweiInterface(ls, i)
			if iface != nil {
				dc.Interfaces = append(dc.Interfaces, iface)
			}
			i += consumed
		}
	}

	sortVlans(dc.Vlans)
	return dc, nil
}

func parseHuaweiInterface(ls []string, start int) (*netgrabv1.Interface, int) {
	name := strings.TrimSpace(strings.TrimSpace(ls[start])[len("interface "):])
	if name == "" {
		return nil, 0
	}
	iface := &netgrabv1.Interface{
		Name:    name,
		Type:    interfaceType(name),
		Enabled: true,
	}

	consumed := 0
	for j := start + 1; j < len(ls); j++ {
		line := ls[j]
		if !isIndented(line) {
			break
		}
		consumed++
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "description "):
			iface.Description = unquote(strings.TrimSpace(t[len("description "):]))
		case t == "shutdown":
			iface.Enabled = false
		case t == "undo shutdown":
			iface.Enabled = true
		case strings.HasPrefix(t, "ip address "):
			// "ip address 10.0.0.1 255.255.255.0" (a trailing "sub" is possible)
			if cidr := parseIPMask(strings.TrimSpace(t[len("ip address "):])); cidr != "" {
				iface.IpAddresses = append(iface.IpAddresses, cidr)
			}
		case strings.HasPrefix(t, "port default vlan "):
			if v, err := strconv.Atoi(strings.Fields(t)[3]); err == nil {
				iface.AccessVlan = uint32(v)
				iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS
			}
		case strings.HasPrefix(t, "port trunk allow-pass vlan "):
			iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_TRUNK
			iface.TaggedVlans = append(iface.TaggedVlans, parseHuaweiVlans(strings.TrimSpace(t[len("port trunk allow-pass vlan "):]))...)
		}
	}
	if iface.Mode == netgrabv1.InterfaceMode_INTERFACE_MODE_UNSPECIFIED && len(iface.IpAddresses) > 0 {
		iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
	}
	return iface, consumed
}

// parseHuaweiRoute parses "ip route-static DEST MASK NEXTHOP ...". VPN-instance
// routes ("ip route-static vpn-instance NAME ...") are skipped — they are not
// part of the global table this model represents.
func parseHuaweiRoute(line string) *netgrabv1.StaticRoute {
	f := strings.Fields(line)
	// f[0]=ip f[1]=route-static f[2]=DEST f[3]=MASK f[4]=NEXTHOP
	if len(f) < 5 || f[2] == "vpn-instance" {
		return nil
	}
	cidr := maskToCIDR(f[2], f[3])
	if cidr == "" {
		return nil
	}
	return &netgrabv1.StaticRoute{Prefix: cidr, NextHop: f[4]}
}

// parseHuaweiVlans parses Huawei VLAN specs: "100", "100 smart", "70 to 74",
// "batch 10 20 30", "70 to 74 131 to 135". "to" denotes an inclusive range.
func parseHuaweiVlans(s string) []uint32 {
	fields := strings.Fields(s)
	var out []uint32
	for i := 0; i < len(fields); i++ {
		w := fields[i]
		switch w {
		case "smart", "batch", "mux", "standard":
			continue
		case "to":
			// handled when we see the low bound; a stray "to" is ignored
			continue
		}
		lo, err := strconv.Atoi(w)
		if err != nil {
			continue
		}
		// Range "lo to hi"?
		if i+2 < len(fields) && fields[i+1] == "to" {
			if hi, err2 := strconv.Atoi(fields[i+2]); err2 == nil && hi >= lo {
				for v := lo; v <= hi; v++ {
					out = append(out, uint32(v))
				}
				i += 2
				continue
			}
		}
		out = append(out, uint32(lo))
	}
	return out
}
