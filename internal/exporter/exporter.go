// Package exporter renders a set of parsed device configs
// (model/netgrabv1.DeviceConfigSet) into downstream formats: JSON, YAML, TOML,
// CSV and Terraform for NetBox.
//
// Exporters never touch protobuf wire types directly; they first project the
// model onto a plain, tag-annotated view (View / DeviceView) so enums become
// readable strings and timestamps become RFC3339. That one projection keeps
// every text format consistent.
package exporter

import (
	"fmt"
	"strings"
	"time"

	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// Exporter renders a DeviceConfigSet to bytes in one format.
type Exporter interface {
	Format() string    // short id: json, yaml, toml, csv, tf
	Extension() string // file extension without the dot
	Export(set *netgrabv1.DeviceConfigSet) ([]byte, error)
}

// Registry of the built-in exporters, keyed by Format().
var registry = map[string]Exporter{}

func register(e Exporter) { registry[e.Format()] = e }

func init() {
	register(JSONExporter{})
	register(YAMLExporter{})
	register(TOMLExporter{})
	register(CSVExporter{})
	register(TerraformExporter{})
}

// Get returns the exporter for a format id (json/yaml/toml/csv/tf).
func Get(format string) (Exporter, bool) {
	e, ok := registry[strings.ToLower(strings.TrimSpace(format))]
	return e, ok
}

// Formats lists the supported format ids.
func Formats() []string {
	return []string{"json", "yaml", "toml", "csv", "tf"}
}

// --- view model: the plain shape every text exporter marshals ---

// View is the top-level export document.
type View struct {
	Devices []DeviceView `json:"devices" yaml:"devices" toml:"devices"`
}

// DeviceView is one device, projected for export.
type DeviceView struct {
	Hostname      string          `json:"hostname" yaml:"hostname" toml:"hostname"`
	Vendor        string          `json:"vendor" yaml:"vendor" toml:"vendor"`
	Platform      string          `json:"platform" yaml:"platform" toml:"platform"`
	Model         string          `json:"model,omitempty" yaml:"model,omitempty" toml:"model,omitempty"`
	OSVersion     string          `json:"os_version,omitempty" yaml:"os_version,omitempty" toml:"os_version,omitempty"`
	Serial        string          `json:"serial,omitempty" yaml:"serial,omitempty" toml:"serial,omitempty"`
	ManagementIP  string          `json:"management_ip,omitempty" yaml:"management_ip,omitempty" toml:"management_ip,omitempty"`
	SourceCommand string          `json:"source_command,omitempty" yaml:"source_command,omitempty" toml:"source_command,omitempty"`
	SHA256        string          `json:"raw_config_sha256,omitempty" yaml:"raw_config_sha256,omitempty" toml:"raw_config_sha256,omitempty"`
	RawBytes      int64           `json:"raw_config_bytes,omitempty" yaml:"raw_config_bytes,omitempty" toml:"raw_config_bytes,omitempty"`
	CollectedAt   string          `json:"collected_at,omitempty" yaml:"collected_at,omitempty" toml:"collected_at,omitempty"`
	Parser        string          `json:"parser,omitempty" yaml:"parser,omitempty" toml:"parser,omitempty"`
	Interfaces    []InterfaceView `json:"interfaces,omitempty" yaml:"interfaces,omitempty" toml:"interfaces,omitempty"`
	Vlans         []VlanView      `json:"vlans,omitempty" yaml:"vlans,omitempty" toml:"vlans,omitempty"`
	Routes        []RouteView     `json:"routes,omitempty" yaml:"routes,omitempty" toml:"routes,omitempty"`
	Warnings      []string        `json:"warnings,omitempty" yaml:"warnings,omitempty" toml:"warnings,omitempty"`
}

// InterfaceView is one interface, projected for export.
type InterfaceView struct {
	Name        string   `json:"name" yaml:"name" toml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty" toml:"description,omitempty"`
	Enabled     bool     `json:"enabled" yaml:"enabled" toml:"enabled"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	MTU         uint32   `json:"mtu,omitempty" yaml:"mtu,omitempty" toml:"mtu,omitempty"`
	MacAddress  string   `json:"mac_address,omitempty" yaml:"mac_address,omitempty" toml:"mac_address,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty" yaml:"ip_addresses,omitempty" toml:"ip_addresses,omitempty"`
	Mode        string   `json:"mode,omitempty" yaml:"mode,omitempty" toml:"mode,omitempty"`
	AccessVlan  uint32   `json:"access_vlan,omitempty" yaml:"access_vlan,omitempty" toml:"access_vlan,omitempty"`
	TaggedVlans []uint32 `json:"tagged_vlans,omitempty" yaml:"tagged_vlans,omitempty" toml:"tagged_vlans,omitempty"`
	Parent      string   `json:"parent,omitempty" yaml:"parent,omitempty" toml:"parent,omitempty"`
}

// VlanView is one VLAN.
type VlanView struct {
	ID   uint32 `json:"id" yaml:"id" toml:"id"`
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
}

// RouteView is one static route.
type RouteView struct {
	Prefix   string `json:"prefix" yaml:"prefix" toml:"prefix"`
	NextHop  string `json:"next_hop" yaml:"next_hop" toml:"next_hop"`
	Distance uint32 `json:"distance,omitempty" yaml:"distance,omitempty" toml:"distance,omitempty"`
}

// ToView projects the protobuf model onto the export view.
func ToView(set *netgrabv1.DeviceConfigSet) View {
	v := View{}
	if set == nil {
		return v
	}
	for _, d := range set.Devices {
		v.Devices = append(v.Devices, deviceToView(d))
	}
	return v
}

func deviceToView(d *netgrabv1.DeviceConfig) DeviceView {
	dv := DeviceView{
		Hostname:      d.Hostname,
		Vendor:        d.Vendor,
		Platform:      d.Platform,
		Model:         d.Model,
		OSVersion:     d.OsVersion,
		Serial:        d.Serial,
		ManagementIP:  d.ManagementIp,
		SourceCommand: d.SourceCommand,
		SHA256:        d.RawConfigSha256,
		RawBytes:      d.RawConfigBytes,
		Parser:        d.Parser,
		Warnings:      d.Warnings,
	}
	if d.CollectedAtUnix > 0 {
		dv.CollectedAt = time.Unix(d.CollectedAtUnix, 0).UTC().Format(time.RFC3339)
	}
	for _, i := range d.Interfaces {
		dv.Interfaces = append(dv.Interfaces, InterfaceView{
			Name:        i.Name,
			Description: i.Description,
			Enabled:     i.Enabled,
			Type:        i.Type,
			MTU:         i.Mtu,
			MacAddress:  i.MacAddress,
			IPAddresses: i.IpAddresses,
			Mode:        modeString(i.Mode),
			AccessVlan:  i.AccessVlan,
			TaggedVlans: i.TaggedVlans,
			Parent:      i.Parent,
		})
	}
	for _, vl := range d.Vlans {
		dv.Vlans = append(dv.Vlans, VlanView{ID: vl.Id, Name: vl.Name})
	}
	for _, r := range d.Routes {
		dv.Routes = append(dv.Routes, RouteView{Prefix: r.Prefix, NextHop: r.NextHop, Distance: r.Distance})
	}
	return dv
}

func modeString(m netgrabv1.InterfaceMode) string {
	switch m {
	case netgrabv1.InterfaceMode_INTERFACE_MODE_ACCESS:
		return "access"
	case netgrabv1.InterfaceMode_INTERFACE_MODE_TRUNK:
		return "trunk"
	case netgrabv1.InterfaceMode_INTERFACE_MODE_ROUTED:
		return "routed"
	default:
		return ""
	}
}

// ExportAll runs every requested format and returns a map of extension->bytes.
func ExportAll(set *netgrabv1.DeviceConfigSet, formats []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, f := range formats {
		e, ok := Get(f)
		if !ok {
			return nil, fmt.Errorf("exporter: unknown format %q", f)
		}
		b, err := e.Export(set)
		if err != nil {
			return nil, fmt.Errorf("exporter %s: %w", f, err)
		}
		out[e.Extension()] = b
	}
	return out, nil
}
