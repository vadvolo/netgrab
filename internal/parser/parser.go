// Package parser turns a raw device configuration dump into netgrab's
// normalized protobuf model (model/netgrabv1.DeviceConfig).
//
// Each supported platform family has its own Parser. Resolve picks one the same
// way connector.ResolveProfile picks a collection profile: most-specific
// platform first, then vendor, then a generic fallback that never fails. That
// mirrors the inventory's ~22 vendors / ~30 platforms so every collected config
// can be turned into at least an identity record.
package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// Input is one device's raw configuration plus the inventory context needed to
// label the resulting model.
type Input struct {
	Hostname      string
	Vendor        string
	Platform      string
	ManagementIP  string
	SourceCommand string
	Raw           []byte
	// CollectedAtUnix is the collection time in unix seconds. The caller stamps
	// it (parsers are pure and deterministic, so they never read the clock).
	CollectedAtUnix int64
}

// Parser converts an Input into a DeviceConfig. Parsers are best-effort: they
// never return an error for merely unfamiliar lines, they record those in
// DeviceConfig.Warnings and extract whatever they can.
type Parser interface {
	// ID is a stable identifier recorded in DeviceConfig.Parser.
	ID() string
	Parse(in Input) (*netgrabv1.DeviceConfig, error)
}

// platformParsers / vendorParsers mirror connector.ResolveProfile's keys so the
// collect and parse stages agree on how a device is classified.
var (
	cisco    = &CiscoParser{}
	routeros = &RouterOSParser{}
	huawei   = &HuaweiParser{}
	airos    = &AirOSParser{}
	cambium  = &CambiumParser{}
	generic  = &GenericParser{}

	platformParsers = map[string]Parser{
		"ios":      cisco,
		"iosxe":    cisco,
		"iosxr":    cisco,
		"nxos":     cisco,
		"mes":      cisco, // Eltex MES CLI is IOS-like
		"ruijie":   cisco,
		"procurve": cisco,
		"zynos":    cisco,
		"ies":      cisco,
		"tplink":   cisco,
		"dcn":      cisco,
		"routeros": routeros,
		"swos":     routeros,
		"smartax":  huawei,
		"olt":      huawei,
		"airos":    airos,
		"epmp":     cambium,
	}

	vendorParsers = map[string]Parser{
		"cisco":    cisco,
		"eltex":    cisco,
		"ruijie":   cisco,
		"hp":       cisco,
		"zyxel":    cisco,
		"tplink":   cisco,
		"dcn":      cisco,
		"ros":      routeros,
		"huawei":   huawei,
		"h3c":      huawei,
		"ubiquiti": airos,
		"cambium":  cambium,
	}
)

// Resolve returns the parser for a device. It always returns a usable parser
// (the generic fallback when nothing more specific matches), so callers never
// have to branch on "unsupported".
func Resolve(vendor, platform string) Parser {
	if p, ok := platformParsers[strings.ToLower(strings.TrimSpace(platform))]; ok {
		return p
	}
	if p, ok := vendorParsers[strings.ToLower(strings.TrimSpace(vendor))]; ok {
		return p
	}
	return generic
}

// Parse is a convenience wrapper that resolves the parser from the input's
// vendor/platform and runs it.
func Parse(in Input) (*netgrabv1.DeviceConfig, error) {
	return Resolve(in.Vendor, in.Platform).Parse(in)
}

// newBase builds the DeviceConfig skeleton common to every parser: identity
// copied from the inventory plus provenance (hash, size, timestamp, parser id).
func newBase(in Input, parserID string) *netgrabv1.DeviceConfig {
	sum := sha256.Sum256(in.Raw)
	return &netgrabv1.DeviceConfig{
		Hostname:        in.Hostname,
		Vendor:          in.Vendor,
		Platform:        in.Platform,
		ManagementIp:    in.ManagementIP,
		SourceCommand:   in.SourceCommand,
		RawConfigSha256: hex.EncodeToString(sum[:]),
		RawConfigBytes:  int64(len(in.Raw)),
		CollectedAtUnix: in.CollectedAtUnix,
		Parser:          parserID,
	}
}

// lines splits raw config into trimmed-of-trailing-CR lines without allocating
// per-line copies of the whole file.
func lines(raw []byte) []string {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	return strings.Split(s, "\n")
}
