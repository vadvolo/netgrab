package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"netgrab/internal/connector"
	"netgrab/internal/inventory"
	"netgrab/internal/storage/gitlab"
)

func main() {
	// Load .env (if present); device and GitLab secrets come from the
	// environment, never the repo. Missing file is not an error.
	_ = godotenv.Load()

	invPath := flag.String("inventory", envOr("DEVICES_INVENTORY", "./config/devices.example.yaml"),
		"path to device inventory YAML (env DEVICES_INVENTORY)")
	flag.Parse()

	observedZapCore, _ := observer.New(zap.InfoLevel)
	logger := zap.New(observedZapCore)

	cfg := gitlab.MustLoad()
	cfg.Logger = logger
	storage := gitlab.NewStorage(cfg)

	inv, err := inventory.Load(*invPath)
	if err != nil {
		log.Fatalf("inventory: %v", err)
	}
	devices, err := connector.FromInventory(inv)
	if err != nil {
		log.Fatalf("build devices: %v", err)
	}

	results := make([]string, len(devices))
	errors := make([]error, len(devices))

	for i, device := range devices {
		err := device.Connect(logger)
		if err != nil {
			errors[i] = err
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		config, err := device.GetConfig(ctx)
		cancel()
		if err != nil {
			errors[i] = err
			continue
		}

		f, err := storage.SaveConfig(config, device.GetName())
		if err != nil {
			errors[i] = err
			continue
		}
		results[i] = f
	}

	println("Results:")
	for _, r := range results {
		println(r)
	}

	println("Errors:")
	for _, e := range errors {
		if e != nil {
			println(e.Error())
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
