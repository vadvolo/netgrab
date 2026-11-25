package connector

import (
	"fmt"

	"netgrab/internal/inventory"
)

// FromInventory builds direct-SSH Device values from a loaded inventory,
// resolving each device's credentials from the inventory's credential groups.
//
// It is used by the local-storage and gitlab-storage collectors, which open SSH
// sessions directly from the host running netgrab (unlike orion-collect, which
// goes through the remote gnetcli server). Vendor selects the connector; every
// non-Huawei vendor uses the Cisco-style connector.
func FromInventory(inv *inventory.Inventory) ([]Device, error) {
	devices := make([]Device, 0, len(inv.Devices))
	for i := range inv.Devices {
		d := &inv.Devices[i]
		creds, ok := inv.CredentialsFor(d)
		if !ok {
			return nil, fmt.Errorf("connector: no credentials configured for device %q (set a matching credentials group, secrets via ${ENV})", d.Hostname)
		}
		switch d.Vendor {
		case "huawei", "h3c":
			devices = append(devices, NewHuaweiDevice(d.Hostname, creds.Login, d.Target(), creds.Password))
		default:
			devices = append(devices, NewCiscoDevice(d.Hostname, creds.Login, d.Target(), creds.Password))
		}
	}
	return devices, nil
}
