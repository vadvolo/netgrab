// Package storage defines where netgrab persists collected artifacts — raw
// device configs, parsed models, and exports — and provides key helpers shared
// by the local-filesystem and S3 (Yandex Cloud) backends.
package storage

import (
	"context"
	"path"
	"strings"
)

// Store is a write-oriented blob store. Keys are '/'-separated relative paths
// (never absolute, no leading slash). Put returns a human-readable locator: a
// filesystem path for the local backend, an "s3://bucket/key" URL for S3.
type Store interface {
	Put(ctx context.Context, key string, data []byte) (locator string, err error)
}

// Artifact kinds, used as the top-level key prefix so raw configs, parsed
// models and exports live in separate trees.
const (
	KindConfig = "configs"
	KindParsed = "parsed"
	KindExport = "exports"
	KindDevice = "devices"
)

// ConfigKey is the key for a device's raw configuration dump.
//
//	configs/<device>/<device>_<timestamp>.txt
func ConfigKey(device, timestamp string) string {
	return path.Join(KindConfig, safe(device), safe(device)+"_"+timestamp+".txt")
}

// ParsedKey is the key for a device's parsed model in the given format
// (json/pb).
//
//	parsed/<device>/<device>_<timestamp>.<ext>
func ParsedKey(device, timestamp, ext string) string {
	return path.Join(KindParsed, safe(device), safe(device)+"_"+timestamp+"."+ext)
}

// DeviceKey is the stable, un-timestamped key for a single device's current
// parsed model, so consumers can GET one device directly without listing:
//
//	devices/<hostname>_<ip>.json
//
// It is overwritten on every collection, so it always holds the latest. When ip
// is empty it is omitted; when hostname is empty the ip alone is used.
func DeviceKey(hostname, ip string) string {
	base := safe(hostname)
	switch {
	case base == "":
		base = safe(ip)
	case ip != "":
		base += "_" + safe(ip)
	}
	return path.Join(KindDevice, base+".json")
}

// ExportKey is the key for a fleet-wide export file.
//
//	exports/<timestamp>/netbox.<ext>  (name="netbox", ext="tf")
func ExportKey(timestamp, name, ext string) string {
	return path.Join(KindExport, timestamp, name+"."+ext)
}

// Multi fans a single Put out to several backends (e.g. local + S3). It returns
// the locators from every backend that succeeded. If every backend fails, the
// first error is returned; a partial failure is reported via Errs so the caller
// can log it without losing the successful writes.
type Multi struct {
	Stores []Store
}

// MultiResult is the outcome of a Multi.Put.
type MultiResult struct {
	Locators []string
	Errs     []error
}

// NewMulti builds a Multi from the non-nil stores given.
func NewMulti(stores ...Store) *Multi {
	var s []Store
	for _, st := range stores {
		if st != nil {
			s = append(s, st)
		}
	}
	return &Multi{Stores: s}
}

// Put writes to every backend. It satisfies Store (returning the first
// locator), but callers wanting per-backend detail should use PutAll.
func (m *Multi) Put(ctx context.Context, key string, data []byte) (string, error) {
	res := m.PutAll(ctx, key, data)
	if len(res.Locators) == 0 {
		if len(res.Errs) > 0 {
			return "", res.Errs[0]
		}
		return "", nil
	}
	return res.Locators[0], nil
}

// PutAll writes to every backend and collects all locators and errors.
func (m *Multi) PutAll(ctx context.Context, key string, data []byte) MultiResult {
	var res MultiResult
	for _, st := range m.Stores {
		loc, err := st.Put(ctx, key, data)
		if err != nil {
			res.Errs = append(res.Errs, err)
			continue
		}
		res.Locators = append(res.Locators, loc)
	}
	return res
}

// safe makes a device name safe to use as a single path segment.
func safe(name string) string {
	r := strings.NewReplacer("/", "_", " ", "_", ":", "_", "\\", "_")
	return r.Replace(name)
}
