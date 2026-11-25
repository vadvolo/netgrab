package gitlab

import (
	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"
	"log"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	URL     string `yaml:"url" env:"GITLAB_URL" env-default:"https://gitlab.com"`
	Project string `yaml:"project" env:"GITLAB_PROJECT" env-default:""`
	Branch  string `yaml:"branch" env:"BRANCH" env-default:"master"`
	Token   string `yaml:"token" env:"GITLAB_TOKEN" env-required:"true"`

	Logger *zap.Logger
}

func expandEnvVars(content string) string {
	envPattern := regexp.MustCompile(`env:([A-Z_]+)`)
	return envPattern.ReplaceAllStringFunc(content, func(match string) string {
		varName := strings.TrimPrefix(match, "env:")
		if value := os.Getenv(varName); value != "" {
			return value
		}
		return match
	})
}

func MustLoad() *Config {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH environment variable must be set")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatal("CONFIG_PATH environment variable must be set")
	}

	//yamlContent, err := io.ReadFile(configPath)
	//if err != nil {
	//	log.Fatal(err)
	//}

	//expandedContent := expandEnvVars(string(yamlContent))
	
	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatal(err)
	}
	return &cfg
}
