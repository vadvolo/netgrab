package parser

import (
	"regexp"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// GenericParser is the catch-all for platforms without a dedicated parser
// (AirOS, Cambium ePMP, Linux hosts, unknown grammars). It never fails: it
// records identity plus a best-effort hostname scrape and leaves the structured
// facts empty, so every collected config still yields a model row.
type GenericParser struct{}

func (p *GenericParser) ID() string { return "generic" }

// hostnamePatterns are loose "name = value" forms seen across the long tail of
// platforms (AirOS resolv.host.1.name, Cambium/Linux hostname, etc.).
var hostnamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?mi)^\s*hostname[:= ]+["']?([^\s"']+)`),
	regexp.MustCompile(`(?mi)^\s*resolv\.host\.1\.name=([^\s]+)`),
	regexp.MustCompile(`(?mi)^\s*system\.name=([^\s]+)`),
	regexp.MustCompile(`(?mi)^\s*set\s+name=([^\s]+)`),
}

func (p *GenericParser) Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	dc := newBase(in, p.ID())
	raw := string(in.Raw)
	if dc.Hostname == "" {
		for _, re := range hostnamePatterns {
			if m := re.FindStringSubmatch(raw); len(m) > 1 {
				dc.Hostname = strings.TrimSpace(m[1])
				break
			}
		}
	}
	dc.Warnings = append(dc.Warnings,
		"no dedicated parser for vendor="+in.Vendor+" platform="+in.Platform+"; identity only")
	return dc, nil
}
