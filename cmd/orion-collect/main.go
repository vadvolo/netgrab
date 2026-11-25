// Command orion-collect runs a local Orion configuration collection.
//
// It loads the Orion inventory, connects to the remote gnetcli server
// (configured via config/env), and — for every supported vendor/model — asks the
// server to run the right "show config" command and saves the result to local
// storage. Collection fans out across a worker pool.
//
// Usage:
//
//	CONFIG_PATH=./config/orion.yaml go run ./cmd/orion-collect
//	go run ./cmd/orion-collect -config ./config/orion.yaml -limit 5
//	go run ./cmd/orion-collect -vendor eltex        # only Eltex devices
//	go run ./cmd/orion-collect -host site-a-eltex-1
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"netgrab/internal/annetoil"
	"netgrab/internal/config"
	"netgrab/internal/connector"
	"netgrab/internal/exporter"
	"netgrab/internal/gnetcli"
	"netgrab/internal/inventory"
	netgrabv1 "netgrab/internal/model/netgrabv1"
	"netgrab/internal/parser"
	"netgrab/internal/storage"
)

func main() {
	_ = godotenv.Load()

	var (
		configPath  = flag.String("config", os.Getenv("CONFIG_PATH"), "path to YAML config (env CONFIG_PATH)")
		vendorFlt   = flag.String("vendor", "", "only collect devices with this vendor")
		platformFlt = flag.String("platform", "", "only collect devices with this platform")
		hostFlt     = flag.String("host", "", "only collect the device with this hostname/IP (substring match)")
		limit       = flag.Int("limit", 0, "stop after this many matching devices (0 = no limit)")
		via         = flag.String("via", "gnetcli", "collection path: 'gnetcli' (direct server) or 'annet-oil' (HTTP execute API; resolves device creds itself)")
		dryRun      = flag.Bool("dry-run", false, "resolve profiles and print the plan without contacting the server")
	)
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()

	cfg, err := config.LoadOrion(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	inv, err := inventory.Load(cfg.InventoryPath)
	if err != nil {
		log.Fatalf("inventory: %v", err)
	}
	logger.Info("inventory loaded",
		zap.Int("devices", len(inv.Devices)),
		zap.String("path", cfg.InventoryPath),
	)

	// collect(ctx, dev, prof, crd) grabs one device's raw config. Which path it
	// uses depends on -via: the gnetcli server directly (netgrab holds the
	// device creds) or annet-oil's execute API (annet-oil holds the creds).
	var collect func(ctx context.Context, dev inventory.Device, prof connector.Profile, crd inventory.Credentials) ([]byte, error)
	viaAnnetOil := strings.EqualFold(*via, "annet-oil") || strings.EqualFold(*via, "annetoil")

	// Clients are only built for a real run; -dry-run just prints the plan.
	switch {
	case *dryRun:
		// no client needed
	case viaAnnetOil:
		ao, aerr := annetoil.New(annetoil.Config{
			BaseURL:        cfg.AnnetOil.BaseURL,
			Token:          cfg.AnnetOil.Token,
			RequestTimeout: time.Duration(cfg.AnnetOil.TimeoutSec * float64(time.Second)),
		})
		if aerr != nil {
			log.Fatalf("annet-oil client: %v", aerr)
		}
		logger.Info("using annet-oil execute API", zap.String("base_url", cfg.AnnetOil.BaseURL))
		collect = func(ctx context.Context, dev inventory.Device, prof connector.Profile, _ inventory.Credentials) ([]byte, error) {
			res, rerr := ao.Exec(ctx, dev.Target(), prof.Command, prof.GnetcliDevice, cfg.AnnetOil.TimeoutSec)
			if rerr != nil {
				return nil, rerr
			}
			if res.Failed() {
				return []byte(res.Output), fmt.Errorf("device error %s: %s", res.ErrorCode, res.Error)
			}
			return []byte(res.Output), nil
		}

	default:
		client, cerr := gnetcli.New(gnetcli.Config{
			Host:           cfg.Gnetcli.Host,
			Port:           cfg.Gnetcli.Port,
			AuthToken:      cfg.Gnetcli.AuthToken,
			Login:          cfg.Gnetcli.Login,
			Password:       cfg.Gnetcli.Password,
			TLS:            cfg.Gnetcli.TLS,
			ReadTimeoutSec: cfg.Gnetcli.ReadTimeoutSec,
			CmdTimeoutSec:  cfg.Gnetcli.CmdTimeoutSec,
		})
		if cerr != nil {
			log.Fatalf("gnetcli client: %v", cerr)
		}
		defer func() { _ = client.Close() }()
		logger.Info("using gnetcli server", zap.String("addr", cfg.Gnetcli.Host))
		collect = func(ctx context.Context, d inventory.Device, _ connector.Profile, crd inventory.Credentials) ([]byte, error) {
			dev := connector.NewOrionDevice(
				client, name(d), d.Target(), d.GetPort(),
				crd.Login, crd.Password, d.Vendor, d.Platform,
			)
			if cerr := dev.Connect(logger); cerr != nil {
				return nil, cerr
			}
			return dev.GetConfig(ctx)
		}
	}

	store, backends, err := storage.Build(context.Background(), cfg.StoragePath, storage.S3Params{
		Enabled:      cfg.S3.Enabled,
		Bucket:       cfg.S3.Bucket,
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		Profile:      cfg.S3.Profile,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		UsePathStyle: cfg.S3.UsePathStyle,
	})
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	logger.Info("storage backends", zap.Strings("targets", backends))

	// One run-wide timestamp keeps a device's raw config and parsed model under
	// matching keys.
	runTS := time.Now().UTC().Format("20060102-150405")

	// Build the work list, applying filters and skipping devices with no
	// collectable profile or no credentials.
	type job struct {
		dev  inventory.Device
		orig connector.Profile
		crd  inventory.Credentials
	}
	var jobs []job
	var skipped, noCreds int64
	for _, d := range inv.Devices {
		if !matches(d, *vendorFlt, *platformFlt, *hostFlt) {
			continue
		}
		profile, ok := connector.ResolveProfile(d.Vendor, d.Platform)
		if !ok || !profile.Supported {
			skipped++
			continue
		}
		// The gnetcli path needs netgrab-side credentials; the annet-oil path
		// resolves credentials server-side, so don't require them here.
		var crd inventory.Credentials
		if !viaAnnetOil {
			c, ok := inv.CredentialsFor(&d)
			if !ok {
				noCreds++
				continue
			}
			crd = c
		}
		jobs = append(jobs, job{dev: d, orig: profile, crd: crd})
		if *limit > 0 && len(jobs) >= *limit {
			break
		}
	}

	logger.Info("collection plan",
		zap.Int("to_collect", len(jobs)),
		zap.Int64("skipped_unsupported", skipped),
		zap.Int64("skipped_no_credentials", noCreds),
	)

	if *dryRun {
		for _, j := range jobs {
			logger.Info("plan",
				zap.String("device", name(j.dev)),
				zap.String("vendor", j.dev.Vendor),
				zap.String("platform", j.dev.Platform),
				zap.String("gnetcli_device", j.orig.GnetcliDevice),
				zap.String("command", j.orig.Command),
			)
		}
		return
	}

	var ok, failed int64
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()

			devName := name(j.dev)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cfgBytes, err := collect(ctx, j.dev, j.orig, j.crd)
			if err != nil {
				logger.Warn("collection failed",
					zap.String("device", devName),
					zap.String("address", j.dev.Target()),
					zap.Error(err),
				)
				atomic.AddInt64(&failed, 1)
				return
			}

			// 1) Store the raw config dump.
			rawKey := storage.ConfigKey(devName, runTS)
			if res := store.PutAll(ctx, rawKey, cfgBytes); len(res.Locators) == 0 {
				logger.Error("save raw failed", zap.String("device", devName), zap.Errors("errs", res.Errs))
				atomic.AddInt64(&failed, 1)
				return
			} else if len(res.Errs) > 0 {
				logger.Warn("save raw partial", zap.String("device", devName), zap.Errors("errs", res.Errs))
			}

			// 2) Parse into the normalized model and store it as protobuf
			//    binary (.pb) plus human-readable protojson (.json).
			model, perr := parser.Parse(parser.Input{
				Hostname:        devName,
				Vendor:          j.dev.Vendor,
				Platform:        j.dev.Platform,
				ManagementIP:    j.dev.Target(),
				SourceCommand:   j.orig.Command,
				Raw:             cfgBytes,
				CollectedAtUnix: time.Now().Unix(),
			})
			if perr != nil {
				logger.Warn("parse failed", zap.String("device", devName), zap.Error(perr))
			} else {
				if pb, merr := proto.Marshal(model); merr == nil {
					_, _ = store.Put(ctx, storage.ParsedKey(devName, runTS, "pb"), pb)
				}
				jsonOpts := protojson.MarshalOptions{Multiline: true, Indent: "  "}
				if js, merr := jsonOpts.Marshal(model); merr == nil {
					_, _ = store.Put(ctx, storage.ParsedKey(devName, runTS, "json"), js)
				}
				// Stable, directly-addressable per-device file (overwritten each
				// run) so consumers can GET one device without listing timestamps.
				if dj, merr := exporter.MarshalDeviceJSON(model); merr == nil {
					_, _ = store.Put(ctx, storage.DeviceKey(devName, j.dev.IP), dj)
				}
			}

			logger.Info("collected",
				zap.String("device", devName),
				zap.Int("bytes", len(cfgBytes)),
				zap.String("key", rawKey),
				zap.Int("interfaces", ifaceCount(model)),
			)
			atomic.AddInt64(&ok, 1)
		}(j)
	}
	wg.Wait()

	logger.Info("collection complete",
		zap.Int64("collected", ok),
		zap.Int64("failed", failed),
		zap.Int64("skipped_unsupported", skipped),
		zap.Int64("skipped_no_credentials", noCreds),
	)
}

// matches applies the CLI filters to a device.
func matches(d inventory.Device, vendor, platform, host string) bool {
	if vendor != "" && !strings.EqualFold(d.Vendor, vendor) {
		return false
	}
	if platform != "" && !strings.EqualFold(d.Platform, platform) {
		return false
	}
	if host != "" {
		h := strings.ToLower(host)
		if !strings.Contains(strings.ToLower(d.Hostname), h) &&
			!strings.Contains(strings.ToLower(d.IP), h) {
			return false
		}
	}
	return true
}

// ifaceCount is nil-safe for logging.
func ifaceCount(m *netgrabv1.DeviceConfig) int {
	if m == nil {
		return 0
	}
	return len(m.Interfaces)
}

// name is a stable, filesystem-friendly identifier for a device.
func name(d inventory.Device) string {
	n := d.Hostname
	if n == "" {
		n = d.IP
	}
	// Local storage builds a directory per device name; keep it path-safe.
	n = strings.ReplaceAll(n, "/", "_")
	n = strings.ReplaceAll(n, " ", "_")
	return n
}
