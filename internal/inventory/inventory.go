// Package inventory loads the Orion device inventory: the list of equipment to
// collect from, plus named credential groups. The file format matches the one
// annet-oil uses (resources/inventory.orion.yaml), so the same source of truth
// can feed both tools.
package inventory

import (
	"os"
	"strings"

	"golang.org/x/xerrors"
	"gopkg.in/yaml.v3"
)

// DefaultSSHPort is used when a device omits an explicit port.
const DefaultSSHPort = 22

// Device is one row of the inventory.
type Device struct {
	Hostname    string   `yaml:"hostname"`
	IP          string   `yaml:"ip"`
	Port        int      `yaml:"port,omitempty"`
	Vendor      string   `yaml:"vendor"`
	Platform    string   `yaml:"platform,omitempty"`
	Role        string   `yaml:"role,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty"`
}

// GetPort returns the device's SSH port, defaulting to 22.
func (d *Device) GetPort() int {
	if d.Port == 0 {
		return DefaultSSHPort
	}
	return d.Port
}

// Target returns the address the gnetcli server should connect to (IP if set,
// else hostname).
func (d *Device) Target() string {
	if d.IP != "" {
		return d.IP
	}
	return d.Hostname
}

// Credentials is a login/password pair.
type Credentials struct {
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
}

// Inventory is the parsed inventory file.
type Inventory struct {
	Devices     []Device               `yaml:"devices"`
	Credentials map[string]Credentials `yaml:"credentials"`
}

// Load reads and parses the inventory at path, expanding ${ENV} references in
// credential values so secrets stay out of the file.
func Load(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, xerrors.Errorf("inventory: reading %s: %w", path, err)
	}

	var inv Inventory
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, xerrors.Errorf("inventory: parsing %s: %w", path, err)
	}

	for name, c := range inv.Credentials {
		c.Login = os.ExpandEnv(c.Login)
		c.Password = os.ExpandEnv(c.Password)
		inv.Credentials[name] = c
	}
	for i := range inv.Devices {
		inv.Devices[i].Vendor = strings.ToLower(strings.TrimSpace(inv.Devices[i].Vendor))
		inv.Devices[i].Platform = strings.ToLower(strings.TrimSpace(inv.Devices[i].Platform))
	}
	return &inv, nil
}

// CredentialsFor resolves the credentials to use for a device: its role group
// if that group exists and has a non-empty login, otherwise the "default"
// group. The bool is false when no usable credentials are configured.
func (inv *Inventory) CredentialsFor(d *Device) (Credentials, bool) {
	if d.Role != "" {
		if c, ok := inv.Credentials[d.Role]; ok && c.Login != "" {
			return c, true
		}
	}
	if c, ok := inv.Credentials["default"]; ok && c.Login != "" {
		return c, true
	}
	return Credentials{}, false
}
