package main

import (
	"context"
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"netgrab/internal/connector"
	"netgrab/internal/inventory"
	"netgrab/internal/storage/local"
)

func main() {
	// Load .env (if present) so device credentials can stay out of the shell
	// history and out of the repo. Missing file is not an error.
	_ = godotenv.Load()

	invPath := flag.String("inventory", envOr("DEVICES_INVENTORY", "./config/devices.example.yaml"),
		"path to device inventory YAML (env DEVICES_INVENTORY)")
	flag.Parse()

	storage := local.NewLocalStorage("./tmp/local-storage", time.DateOnly)

	observedZapCore, _ := observer.New(zap.InfoLevel)
	logger := zap.New(observedZapCore)

	inv, err := inventory.Load(*invPath)
	if err != nil {
		log.Fatalf("inventory: %v", err)
	}
	devices, err := connector.FromInventory(inv)
	if err != nil {
		log.Fatalf("build devices: %v", err)
	}

	var wg sync.WaitGroup
	for _, device := range devices {
		wg.Add(1)
		go func(dev connector.Device) {
			defer wg.Done()
			if err := dev.Connect(logger); err != nil {
				log.Printf("%s: connect: %v", dev.GetName(), err)
				return
			}
			config, err := dev.GetConfig(context.Background())
			if err != nil {
				log.Printf("%s: get config: %v", dev.GetName(), err)
				return
			}

			f, err := storage.SaveConfigToFile(config, dev.GetName())
			if err != nil {
				log.Printf("%s: save: %v", dev.GetName(), err)
				return
			}
			log.Println(f)
		}(device)
	}
	wg.Wait()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
