package config

import (
	"fmt"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
)

// GnetcliServer describes the remote gnetcli gRPC server that performs the SSH
// sessions on netgrab's behalf. The gnetcli_server listens on 0.0.0.0:50051
// with basic auth; configure the host via config/env (no default host so no
// environment is baked into the binary).
type GnetcliServer struct {
	Host string `yaml:"host" env:"GNETCLI_HOST"`
	Port int    `yaml:"port" env:"GNETCLI_PORT" env-default:"50051"`
	// AuthToken is a pre-encoded base64("login:password"); if set it wins over
	// Login/Password.
	AuthToken string `yaml:"auth_token" env:"GNETCLI_AUTH_TOKEN"`
	Login     string `yaml:"login" env:"GNETCLI_LOGIN"`
	Password  string `yaml:"password" env:"GNETCLI_PASSWORD"`
	TLS       bool   `yaml:"tls" env:"GNETCLI_TLS" env-default:"false"`

	// Per-command timeouts in seconds; 0 inherits the server default.
	ReadTimeoutSec float64 `yaml:"read_timeout_sec" env:"GNETCLI_READ_TIMEOUT_SEC" env-default:"0"`
	CmdTimeoutSec  float64 `yaml:"cmd_timeout_sec" env:"GNETCLI_CMD_TIMEOUT_SEC" env-default:"0"`
}

// S3Storage configures the S3-compatible object store (any S3 API: AWS S3,
// MinIO, a cloud object store, …). When Enabled, collected configs, parsed
// models and exports are written to S3 in addition to local storage. Access
// keys come from the standard AWS env vars (AWS_ACCESS_KEY_ID /
// AWS_SECRET_ACCESS_KEY) or a shared-config Profile. An empty Endpoint uses the
// AWS default endpoint for the region.
type S3Storage struct {
	Enabled      bool   `yaml:"enabled" env:"S3_ENABLED" env-default:"false"`
	Bucket       string `yaml:"bucket" env:"S3_BUCKET" env-default:"netgrab"`
	Endpoint     string `yaml:"endpoint" env:"S3_ENDPOINT"`
	Region       string `yaml:"region" env:"S3_REGION" env-default:"us-east-1"`
	Profile      string `yaml:"profile" env:"S3_PROFILE"`
	AccessKey    string `yaml:"access_key" env:"AWS_ACCESS_KEY_ID"`
	SecretKey    string `yaml:"secret_key" env:"AWS_SECRET_ACCESS_KEY"`
	UsePathStyle bool   `yaml:"use_path_style" env:"S3_USE_PATH_STYLE" env-default:"false"`
}

// AnnetOil describes annet-oil's HTTP execute API, an alternative collection
// path. annet-oil already owns the Orion inventory and every device's
// credentials, so netgrab can POST just a host + command and let annet-oil
// resolve creds and run the session — no device secrets in netgrab.
type AnnetOil struct {
	// BaseURL is the API root including the version prefix. Configure via
	// config/env; no default so no environment is baked into the binary.
	BaseURL string `yaml:"base_url" env:"ANNETOIL_BASE_URL"`
	// Token is the bearer token (Authorization: Bearer <token>).
	Token string `yaml:"token" env:"ANNETOIL_TOKEN"`
	// TimeoutSec bounds a single execute call; 0 uses the client default.
	TimeoutSec float64 `yaml:"timeout_sec" env:"ANNETOIL_TIMEOUT_SEC" env-default:"0"`
}

// OrionConfig configures a local Orion config-collection run.
type OrionConfig struct {
	// InventoryPath is the Orion inventory YAML (same format annet-oil uses).
	InventoryPath string        `yaml:"inventoryPath" env:"INVENTORY_PATH" env-default:"./config/inventory.orion.yaml"`
	StoragePath   string        `yaml:"storagePath" env:"STORAGE_PATH" env-default:"./tmp/local-storage"`
	Concurrency   int           `yaml:"concurrency" env:"COLLECT_CONCURRENCY" env-default:"20"`
	Gnetcli       GnetcliServer `yaml:"gnetcli"`
	AnnetOil      AnnetOil      `yaml:"annetoil"`
	S3            S3Storage     `yaml:"s3"`
}

// LoadOrion reads the Orion collector config. If path is non-empty it is read
// as YAML (with env overrides); otherwise config is taken from environment and
// defaults only. Device/server secrets are expected to come from the
// environment, never committed to the file.
func LoadOrion(path string) (*OrionConfig, error) {
	var cfg OrionConfig
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("config file %s: %w", path, err)
		}
		if err := cleanenv.ReadConfig(path, &cfg); err != nil {
			return nil, fmt.Errorf("reading config %s: %w", path, err)
		}
	} else if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("reading config from env: %w", err)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 20
	}
	return &cfg, nil
}
