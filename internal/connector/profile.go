package connector

import "strings"

// Profile tells the collector how to talk to one class of Orion equipment:
// which gnetcli device profile the server should load, and which command dumps
// the full configuration.
//
// Profile is what "support of all types and models" means in practice: the
// Orion inventory spans ~22 vendors / ~30 platforms (RouterOS, AirOS, Cisco
// IOS/XE/XR/NX-OS, Eltex MES, Huawei OLT, Ruijie, HP ProCurve, Zyxel, ...), and
// each needs its own config command and the right CLI grammar on the server.
type Profile struct {
	// GnetcliDevice is the device-profile name sent to the gnetcli server as
	// HostParams.Device. It must be a profile the server knows (a gnetcli
	// built-in — cisco, nxos, huawei, h3c, juniper, arista, bcomos, ros, pc —
	// or one added via the server's dev-conf, e.g. eltex).
	GnetcliDevice string
	// Command dumps the full running configuration for this platform.
	Command string
	// Supported is false for device classes that have no meaningful text
	// configuration reachable over the gnetcli CLI path (UPS/PDU controllers,
	// Windows/Linux hosts, optical transport, BMCs). The collector skips them
	// instead of hammering them with a command they cannot answer.
	Supported bool
	// Note documents a best-effort or approximate mapping.
	Note string
}

// platformProfiles is keyed by the inventory `platform` field (most specific).
var platformProfiles = map[string]Profile{
	// Layer 2/3 network gear with a real config dump.
	"routeros": {GnetcliDevice: "ros", Command: "/export", Supported: true},
	"swos":     {GnetcliDevice: "ros", Command: "/export", Supported: true, Note: "MikroTik SwOS; export best-effort"},
	"ios":      {GnetcliDevice: "cisco", Command: "show running-config", Supported: true},
	"iosxe":    {GnetcliDevice: "cisco", Command: "show running-config", Supported: true},
	"iosxr":    {GnetcliDevice: "cisco", Command: "show running-config", Supported: true},
	"nxos":     {GnetcliDevice: "nxos", Command: "show running-config", Supported: true},
	"mes":      {GnetcliDevice: "eltex", Command: "show running-config", Supported: true, Note: "Eltex MES; needs eltex profile in server dev-conf, else falls back"},
	"smartax":  {GnetcliDevice: "huawei", Command: "display saved-configuration", Supported: true, Note: "Huawei SmartAX OLT (MA5800): current-configuration is rejected; saved-configuration holds the full config"},
	"olt":      {GnetcliDevice: "huawei", Command: "display saved-configuration", Supported: true, Note: "Huawei OLT: use saved-configuration"},
	"ruijie":   {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "Ruijie is IOS-like"},
	"procurve": {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "HP ProCurve is IOS-like"},
	"zynos":    {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "Zyxel ZyNOS is IOS-like"},
	"ies":      {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "Zyxel IES is IOS-like"},
	"tplink":   {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "TP-Link managed switch is IOS-like"},
	"dcn":      {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "DCN switch is IOS-like"},
	"bcomos":   {GnetcliDevice: "bcomos", Command: "show running-config", Supported: true, Note: "B4COM OS; IOS-like, gnetcli built-in profile"},

	// Wireless / CPE reachable as plain hosts (pc profile == raw shell).
	"airos":  {GnetcliDevice: "pc", Command: "cat /tmp/system.cfg", Supported: true, Note: "Ubiquiti AirOS config file"},
	"epmp":   {GnetcliDevice: "pc", Command: "config show", Supported: true, Note: "Cambium ePMP; best-effort"},
	"mimosa": {GnetcliDevice: "pc", Command: "config show", Supported: true, Note: "Mimosa; best-effort"},

	// Generic hosts: best-effort, still text config.
	"linux":   {GnetcliDevice: "pc", Command: "cat /etc/network/interfaces", Supported: true, Note: "Linux host; interfaces file best-effort"},
	"generic": {GnetcliDevice: "pc", Command: "show running-config", Supported: true, Note: "unknown grammar; best-effort"},

	// No meaningful CLI config dump — skipped.
	"windows":    {Supported: false, Note: "Windows host: no CLI config"},
	"apc":        {Supported: false, Note: "APC PDU/UPS: no running-config"},
	"netagent":   {Supported: false, Note: "UPS network card: no running-config"},
	"netmanplus": {Supported: false, Note: "UPS network card: no running-config"},
	"netbotz":    {Supported: false, Note: "APC NetBotz env monitor: no running-config"},
	"pcoweb":     {Supported: false, Note: "Carel pCOweb HVAC controller: no running-config"},
	"iqnos":      {Supported: false, Note: "Padtec optical transport: unsupported"},
	"irmc":       {Supported: false, Note: "Fujitsu iRMC BMC: no running-config"},
	"ping":       {Supported: false, Note: "monitor-only placeholder platform"},
}

// vendorProfiles is the fallback keyed by the inventory `vendor` field, used
// when the platform is empty or unknown.
var vendorProfiles = map[string]Profile{
	"ros":      {GnetcliDevice: "ros", Command: "/export", Supported: true},
	"cisco":    {GnetcliDevice: "cisco", Command: "show running-config", Supported: true},
	"eltex":    {GnetcliDevice: "eltex", Command: "show running-config", Supported: true},
	"b4com":    {GnetcliDevice: "bcomos", Command: "show running-config", Supported: true, Note: "B4COM; IOS-like, gnetcli built-in profile"},
	"bcomos":   {GnetcliDevice: "bcomos", Command: "show running-config", Supported: true, Note: "B4COM; IOS-like, gnetcli built-in profile"},
	"huawei":   {GnetcliDevice: "huawei", Command: "display current-configuration", Supported: true},
	"h3c":      {GnetcliDevice: "h3c", Command: "display current-configuration", Supported: true},
	"juniper":  {GnetcliDevice: "juniper", Command: "show configuration", Supported: true},
	"arista":   {GnetcliDevice: "arista", Command: "show running-config", Supported: true},
	"ruijie":   {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "IOS-like"},
	"ubiquiti": {GnetcliDevice: "pc", Command: "cat /tmp/system.cfg", Supported: true, Note: "AirOS config file"},
	"cambium":  {GnetcliDevice: "pc", Command: "config show", Supported: true, Note: "best-effort"},
	"mimosa":   {GnetcliDevice: "pc", Command: "config show", Supported: true, Note: "best-effort"},
	"hp":       {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "ProCurve IOS-like"},
	"zyxel":    {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "IOS-like"},
	"tplink":   {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "IOS-like"},
	"dcn":      {GnetcliDevice: "cisco", Command: "show running-config", Supported: true, Note: "IOS-like"},
	"linux":    {GnetcliDevice: "pc", Command: "cat /etc/network/interfaces", Supported: true, Note: "best-effort"},
	"generic":  {GnetcliDevice: "pc", Command: "show running-config", Supported: true, Note: "best-effort"},

	"windows": {Supported: false, Note: "no CLI config"},
	"apc":     {Supported: false, Note: "PDU/UPS: no running-config"},
	"carel":   {Supported: false, Note: "HVAC controller: no running-config"},
	"padtec":  {Supported: false, Note: "optical transport: unsupported"},
	"fujitsu": {Supported: false, Note: "BMC: no running-config"},
	"netman":  {Supported: false, Note: "UPS card: no running-config"},
	"teracom": {Supported: false, Note: "unsupported"},
	"wiseway": {Supported: false, Note: "unsupported"},
}

// ResolveProfile picks the collection profile for a device, preferring the more
// specific platform mapping and falling back to the vendor. The second return
// value is false when neither is known, so the caller can log and skip rather
// than guessing a command.
func ResolveProfile(vendor, platform string) (Profile, bool) {
	if p, ok := platformProfiles[strings.ToLower(strings.TrimSpace(platform))]; ok {
		return p, true
	}
	if p, ok := vendorProfiles[strings.ToLower(strings.TrimSpace(vendor))]; ok {
		return p, true
	}
	return Profile{}, false
}
