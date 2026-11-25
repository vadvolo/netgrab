// Command netgrab-export turns stored device configs into downstream formats:
// JSON, YAML, TOML, CSV and Terraform for NetBox.
//
// It reads the parsed protobuf models the collector writes under
// <storage>/parsed/<device>/*.pb (latest per device), aggregates them into one
// DeviceConfigSet, and writes each requested export via the same storage
// backends as collection (local + optional S3) under exports/<timestamp>/.
//
// If no parsed models exist yet (or -raw-dir is given), it falls back to
// parsing raw "*.txt" config dumps, resolving each device's vendor/platform
// from the inventory by hostname, or from -vendor/-platform when unmatched.
//
// Usage:
//
//	go run ./cmd/netgrab-export -config ./config/orion.yaml
//	go run ./cmd/netgrab-export -format tf,json -storage ./tmp/local-storage
//	go run ./cmd/netgrab-export -raw-dir ./tmp/local-storage -vendor cisco -platform ios
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"netgrab/internal/config"
	"netgrab/internal/exporter"
	"netgrab/internal/inventory"
	netgrabv1 "netgrab/internal/model/netgrabv1"
	"netgrab/internal/parser"
	"netgrab/internal/storage"
)

func main() {
	_ = godotenv.Load()

	var (
		configPath  = flag.String("config", os.Getenv("CONFIG_PATH"), "path to YAML config (env CONFIG_PATH)")
		storagePath = flag.String("storage", "", "storage root to read parsed models from (default: config storagePath)")
		formatsFlt  = flag.String("format", strings.Join(exporter.Formats(), ","), "comma-separated export formats: json,yaml,toml,csv,tf")
		rawDir      = flag.String("raw-dir", "", "parse raw *.txt configs under this dir instead of parsed .pb models")
		vendorDef   = flag.String("vendor", "", "default vendor for raw configs not found in the inventory")
		platformDef = flag.String("platform", "", "default platform for raw configs not found in the inventory")
		perDevice   = flag.Bool("per-device", true, "also write stable per-device files devices/<name>_<ip>.json")
	)
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()

	cfg, err := config.LoadOrion(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	root := *storagePath
	if root == "" {
		root = cfg.StoragePath
	}

	formats := splitCSV(*formatsFlt)

	// Build the device set (names run parallel to set.Devices: the inventory
	// device name used for stable per-device keys).
	var set *netgrabv1.DeviceConfigSet
	var names []string
	if *rawDir != "" {
		inv, _ := inventory.Load(cfg.InventoryPath) // best-effort; nil tolerated
		set, names = fromRaw(logger, *rawDir, inv, *vendorDef, *platformDef)
	} else {
		set, names = fromParsed(logger, root)
		if len(set.Devices) == 0 {
			logger.Warn("no parsed models found; falling back to raw configs", zap.String("root", root))
			inv, _ := inventory.Load(cfg.InventoryPath)
			set, names = fromRaw(logger, root, inv, *vendorDef, *platformDef)
		}
	}
	if len(set.Devices) == 0 {
		log.Fatalf("no device configs found under %s", root)
	}
	logger.Info("loaded devices", zap.Int("count", len(set.Devices)))

	// Render exports.
	outputs, err := exporter.ExportAll(set, formats)
	if err != nil {
		log.Fatalf("export: %v", err)
	}

	// Write via local + optional S3, same as collection.
	ctx := context.Background()
	store, backends, err := storage.Build(ctx, root, storage.S3Params{
		Enabled: cfg.S3.Enabled, Bucket: cfg.S3.Bucket, Endpoint: cfg.S3.Endpoint,
		Region: cfg.S3.Region, Profile: cfg.S3.Profile, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, UsePathStyle: cfg.S3.UsePathStyle,
	})
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	logger.Info("export backends", zap.Strings("targets", backends))

	runTS := time.Now().UTC().Format("20060102-150405")
	for ext, data := range outputs {
		name := "devices"
		if ext == "tf" {
			name = "netbox"
		}
		key := storage.ExportKey(runTS, name, ext)
		res := store.PutAll(ctx, key, data)
		if len(res.Locators) == 0 {
			logger.Error("export write failed", zap.String("ext", ext), zap.Errors("errs", res.Errs))
			continue
		}
		logger.Info("exported", zap.String("ext", ext), zap.Int("bytes", len(data)), zap.Strings("to", res.Locators))
	}

	// Per-device files at stable keys (devices/<name>_<ip>.json), so another
	// service can fetch one device directly. Overwrites any previous version.
	if *perDevice {
		var wrote, failed int
		for i, dc := range set.Devices {
			dj, merr := exporter.MarshalDeviceJSON(dc)
			if merr != nil {
				failed++
				continue
			}
			key := storage.DeviceKey(names[i], dc.ManagementIp)
			if r := store.PutAll(ctx, key, dj); len(r.Locators) == 0 {
				failed++
				continue
			}
			wrote++
		}
		logger.Info("per-device files written", zap.Int("wrote", wrote), zap.Int("failed", failed))
	}
}

// fromParsed loads the latest parsed .pb model per device under
// <root>/parsed/<device>/*.pb.
func fromParsed(logger *zap.Logger, root string) (*netgrabv1.DeviceConfigSet, []string) {
	set := &netgrabv1.DeviceConfigSet{}
	var names []string
	base := filepath.Join(root, storage.KindParsed)
	entries, err := os.ReadDir(base)
	if err != nil {
		return set, names
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pb := latestFile(filepath.Join(base, e.Name()), ".pb")
		if pb == "" {
			continue
		}
		data, err := os.ReadFile(pb)
		if err != nil {
			logger.Warn("read parsed failed", zap.String("file", pb), zap.Error(err))
			continue
		}
		dc := &netgrabv1.DeviceConfig{}
		if err := proto.Unmarshal(data, dc); err != nil {
			logger.Warn("unmarshal parsed failed", zap.String("file", pb), zap.Error(err))
			continue
		}
		set.Devices = append(set.Devices, dc)
		names = append(names, e.Name()) // parsed dir name == inventory device name
	}
	return set, names
}

// fromRaw walks root for *.txt config dumps and parses each. Device name is the
// file's parent directory (or the file stem); vendor/platform come from the
// inventory when the name matches, else from the supplied defaults.
func fromRaw(logger *zap.Logger, root string, inv *inventory.Inventory, vendorDef, platformDef string) (*netgrabv1.DeviceConfigSet, []string) {
	set := &netgrabv1.DeviceConfigSet{}
	var outNames []string
	byName := map[string]inventory.Device{}
	if inv != nil {
		for _, d := range inv.Devices {
			if d.Hostname != "" {
				byName[strings.ToLower(d.Hostname)] = d
			}
		}
	}

	// Only consider the latest .txt per device directory to avoid exporting
	// historical snapshots of the same device.
	dirs := map[string]string{} // deviceName -> latest file
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		name := deviceNameFromPath(root, path)
		if cur, ok := dirs[name]; !ok || filepath.Base(path) > filepath.Base(cur) {
			dirs[name] = path
		}
		return nil
	})

	var names []string
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		path := dirs[name]
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		vendor, platform, mgmt := vendorDef, platformDef, ""
		if dev, ok := byName[strings.ToLower(name)]; ok {
			vendor, platform, mgmt = dev.Vendor, dev.Platform, dev.Target()
		}
		dc, err := parser.Parse(parser.Input{
			Hostname: name, Vendor: vendor, Platform: platform, ManagementIP: mgmt,
			SourceCommand: "offline", Raw: raw, CollectedAtUnix: time.Now().Unix(),
		})
		if err != nil {
			logger.Warn("parse failed", zap.String("device", name), zap.Error(err))
			continue
		}
		set.Devices = append(set.Devices, dc)
		outNames = append(outNames, name)
	}
	return set, outNames
}

// deviceNameFromPath derives a device name from a raw config path: the first
// path segment under root (the per-device directory), or the file stem.
func deviceNameFromPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	parts := strings.Split(rel, string(filepath.Separator))
	// Skip a leading "configs" prefix if present.
	if len(parts) > 1 && parts[0] == storage.KindConfig {
		parts = parts[1:]
	}
	if len(parts) >= 2 {
		return parts[0]
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".txt")
	// Collected files look like "<name>_config_<ts>.txt".
	if i := strings.Index(stem, "_config_"); i > 0 {
		return stem[:i]
	}
	return stem
}

// latestFile returns the lexicographically-largest file with ext in dir, or "".
func latestFile(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(dir, best)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
