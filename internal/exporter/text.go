package exporter

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
	netgrabv1 "netgrab/internal/model/netgrabv1"
)

// JSONExporter renders the view as indented JSON.
type JSONExporter struct{}

func (JSONExporter) Format() string    { return "json" }
func (JSONExporter) Extension() string { return "json" }
func (JSONExporter) Export(set *netgrabv1.DeviceConfigSet) ([]byte, error) {
	b, err := json.MarshalIndent(ToView(set), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// MarshalDeviceJSON renders a single device as the same clean, snake_case JSON
// used in the fleet export — but as a bare object (not wrapped in {"devices":…}).
// It backs the per-device devices/<hostname>_<ip>.json artifact.
func MarshalDeviceJSON(dc *netgrabv1.DeviceConfig) ([]byte, error) {
	b, err := json.MarshalIndent(deviceToView(dc), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// YAMLExporter renders the view as YAML.
type YAMLExporter struct{}

func (YAMLExporter) Format() string    { return "yaml" }
func (YAMLExporter) Extension() string { return "yaml" }
func (YAMLExporter) Export(set *netgrabv1.DeviceConfigSet) ([]byte, error) {
	return yaml.Marshal(ToView(set))
}

// TOMLExporter renders the view as TOML.
type TOMLExporter struct{}

func (TOMLExporter) Format() string    { return "toml" }
func (TOMLExporter) Extension() string { return "toml" }
func (TOMLExporter) Export(set *netgrabv1.DeviceConfigSet) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(ToView(set)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CSVExporter renders a flat, one-row-per-interface table (devices with no
// interfaces still get a row, with empty interface columns), which is the
// shape most spreadsheet / NetBox-import workflows expect.
type CSVExporter struct{}

func (CSVExporter) Format() string    { return "csv" }
func (CSVExporter) Extension() string { return "csv" }
func (CSVExporter) Export(set *netgrabv1.DeviceConfigSet) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{
		"hostname", "vendor", "platform", "model", "os_version", "serial",
		"management_ip", "collected_at", "parser",
		"interface", "if_description", "if_enabled", "if_type", "if_mtu",
		"if_mac", "if_ip_addresses", "if_mode", "if_access_vlan", "if_tagged_vlans",
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, d := range ToView(set).Devices {
		base := []string{
			d.Hostname, d.Vendor, d.Platform, d.Model, d.OSVersion, d.Serial,
			d.ManagementIP, d.CollectedAt, d.Parser,
		}
		if len(d.Interfaces) == 0 {
			if err := w.Write(append(base, "", "", "", "", "", "", "", "", "", "")); err != nil {
				return nil, err
			}
			continue
		}
		for _, i := range d.Interfaces {
			row := append(append([]string{}, base...),
				i.Name, i.Description, strconv.FormatBool(i.Enabled), i.Type,
				uintOrEmpty(i.MTU), i.MacAddress, strings.Join(i.IPAddresses, " "),
				i.Mode, uintOrEmpty(i.AccessVlan), joinUints(i.TaggedVlans),
			)
			if err := w.Write(row); err != nil {
				return nil, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func uintOrEmpty(v uint32) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}

func joinUints(v []uint32) string {
	if len(v) == 0 {
		return ""
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatUint(uint64(x), 10)
	}
	return strings.Join(parts, " ")
}
