package parser

import (
	"strconv"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// RouterOSParser understands MikroTik RouterOS "/export" output, the dominant
// platform in the Orion fleet (~12k devices). The export is a flat list of
// configuration commands grouped under "/path" section headers, each command a
// run of key=value attributes (quoted when they contain spaces), with long
// lines wrapped using a trailing backslash.
type RouterOSParser struct{}

func (p *RouterOSParser) ID() string { return "mikrotik-routeros" }

func (p *RouterOSParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())
	ls := unwrap(lines(in.Raw))

	// Interfaces are referenced by name across sections (/ip address attaches to
	// /interface vlan etc.), so index them as we go.
	byName := map[string]*netgrabv1.Interface{}
	getIface := func(name string) *netgrabv1.Interface {
		if name == "" {
			return nil
		}
		if iface, ok := byName[name]; ok {
			return iface
		}
		iface := &netgrabv1.Interface{Name: name, Type: interfaceType(name), Enabled: true}
		byName[name] = iface
		dc.Interfaces = append(dc.Interfaces, iface)
		return iface
	}

	section := ""
	for _, line := range ls {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			readROSComment(dc, t)
			continue
		}
		if strings.HasPrefix(t, "/") {
			section = t
			continue
		}

		attrs := tokenizeKV(t)
		switch section {
		case "/system identity":
			if n, ok := attrs["name"]; ok {
				dc.Hostname = n
			}

		case "/interface ethernet", "/interface wireless", "/interface bridge",
			"/interface bonding", "/interface list", "/interface eoip",
			"/interface gre", "/interface wireguard":
			name := rosName(attrs)
			if name == "" {
				continue
			}
			iface := getIface(name)
			if t := rosSectionType(section); t != "" {
				iface.Type = t
			}
			applyROSIfaceAttrs(iface, attrs)

		case "/interface vlan":
			name := rosName(attrs)
			if name == "" {
				continue
			}
			iface := getIface(name)
			iface.Type = "vlan"
			applyROSIfaceAttrs(iface, attrs)
			if vid, ok := attrs["vlan-id"]; ok {
				if v, err := strconv.Atoi(vid); err == nil {
					iface.AccessVlan = uint32(v)
					iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS
					addVlan(dc, uint32(v), name)
				}
			}
			if parent, ok := attrs["interface"]; ok {
				iface.Parent = parent
			}

		case "/ip address", "/ipv6 address":
			if addr, ok := attrs["address"]; ok {
				iface := getIface(attrs["interface"])
				if iface != nil {
					if attrs["disabled"] == "yes" {
						// address present but disabled — still record it
					}
					iface.IpAddresses = append(iface.IpAddresses, addr)
					if iface.Mode == netgrabv1.InterfaceMode_INTERFACE_MODE_UNSPECIFIED {
						iface.Mode = netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED
					}
				}
			}

		case "/ip route", "/ipv6 route":
			dst := attrs["dst-address"]
			gw := attrs["gateway"]
			if dst != "" && gw != "" {
				r := &netgrabv1.StaticRoute{Prefix: dst, NextHop: gw}
				if d, err := strconv.Atoi(attrs["distance"]); err == nil {
					r.Distance = uint32(d)
				}
				dc.Routes = append(dc.Routes, r)
			}
		}
	}

	sortVlans(dc.Vlans)
	return dc, nil
}

// applyROSIfaceAttrs maps the common RouterOS interface attributes.
func applyROSIfaceAttrs(iface *netgrabv1.Interface, attrs map[string]string) {
	if c, ok := attrs["comment"]; ok && iface.Description == "" {
		iface.Description = c
	}
	if attrs["disabled"] == "yes" {
		iface.Enabled = false
	}
	if m, ok := attrs["mtu"]; ok {
		if v, err := strconv.Atoi(m); err == nil {
			iface.Mtu = uint32(v)
		}
	}
	if mac, ok := attrs["mac-address"]; ok {
		iface.MacAddress = mac
	}
}

// rosSectionType maps a RouterOS "/interface <kind>" section to a model
// interface type. Empty means "keep the name-based guess" (ethernet section
// keeps whatever interfaceType inferred, e.g. renamed physical ports).
func rosSectionType(section string) string {
	switch section {
	case "/interface bridge":
		return "bridge"
	case "/interface bonding":
		return "lag"
	case "/interface wireless":
		return "wlan"
	case "/interface eoip", "/interface gre", "/interface wireguard":
		return "tunnel"
	default: // "/interface ethernet", "/interface list"
		return ""
	}
}

// rosName returns the interface name from an add/set line. "set" against a
// found default-name has no name= attr, so fall back to default-name.
func rosName(attrs map[string]string) string {
	if n, ok := attrs["name"]; ok {
		return n
	}
	return attrs["default-name"]
}

func addVlan(dc *netgrabv1.DeviceConfig, id uint32, name string) {
	for _, v := range dc.Vlans {
		if v.Id == id {
			return
		}
	}
	dc.Vlans = append(dc.Vlans, &netgrabv1.Vlan{Id: id, Name: name})
}

// readROSComment pulls model / serial / version hints out of the "#" header
// comments RouterOS puts at the top of an export.
func readROSComment(dc *netgrabv1.DeviceConfig, t string) {
	low := strings.ToLower(t)
	switch {
	case strings.Contains(low, "model ="):
		dc.Model = valueAfter(t, "=")
	case strings.Contains(low, "model:"):
		dc.Model = valueAfter(t, ":")
	case strings.Contains(low, "serial number ="):
		dc.Serial = valueAfter(t, "=")
	case strings.Contains(low, "routeros") && dc.OsVersion == "":
		// e.g. "# RouterOS 6.49.10" or "#RouterOS 7.11"
		idx := strings.Index(low, "routeros")
		dc.OsVersion = strings.TrimSpace(t[idx+len("routeros"):])
	case strings.Contains(low, "software id ="):
		if dc.Serial == "" {
			dc.Serial = valueAfter(t, "=")
		}
	}
}

func valueAfter(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return strings.TrimSpace(s[i+len(sep):])
	}
	return ""
}

// unwrap joins RouterOS line continuations: a line ending in "\" continues on
// the next line.
func unwrap(ls []string) []string {
	var out []string
	var buf strings.Builder
	cont := false
	for _, l := range ls {
		trimmedRight := strings.TrimRight(l, " ")
		if cont {
			buf.WriteString(strings.TrimLeft(l, " "))
		} else {
			buf.Reset()
			buf.WriteString(l)
		}
		if strings.HasSuffix(trimmedRight, "\\") {
			// drop the trailing backslash and keep accumulating
			s := buf.String()
			buf.Reset()
			buf.WriteString(strings.TrimRight(strings.TrimRight(s, " "), "\\"))
			cont = true
			continue
		}
		out = append(out, buf.String())
		cont = false
	}
	if cont {
		out = append(out, buf.String())
	}
	return out
}

// tokenizeKV splits a RouterOS command line into key=value attributes,
// respecting double-quoted values that contain spaces. Bare flags (no "=") and
// the leading add/set verb are ignored.
func tokenizeKV(line string) map[string]string {
	attrs := map[string]string{}
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ' ' && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}

	for _, tok := range tokens {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			continue // verb (add/set) or bare flag
		}
		key := tok[:eq]
		val := strings.Trim(tok[eq+1:], "\"")
		attrs[key] = val
	}
	return attrs
}
