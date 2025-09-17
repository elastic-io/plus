package config

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen       string                `yaml:"listen"`
	StoragePath  string                `yaml:"storage-path"`
	DatabasePath string                `yaml:"database-path"`
	Auth         AuthConfig            `yaml:"auth"`
	Cache        CacheConfig           `yaml:"cache"`
	Repositories map[string]RepoConfig `yaml:"repositories"`
	Limits       LimitsConfig          `yaml:"limits"`
	Storage      StorageConfig         `yaml:"storage"`
	DevMode      bool                  `yaml:"dev-mode"`
	Log          string                `yaml:"log"`
	LogLevel     string                `yaml:"log-level"`
}

type AuthConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Token           string `yaml:"token"`
	APIKey          string `yaml:"api-key"`
	RequireReadAuth bool   `yaml:"require-read-auth"`
}

type CacheConfig struct {
	Enabled bool   `yaml:"enabled"`
	TTL     string `yaml:"ttl"`
	MaxSize int    `yaml:"max-size"`
}

type RepoConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"` // rpm, deb
	Enabled     bool   `yaml:"enabled"`
	AutoRefresh bool   `yaml:"auto-refresh"`
}

type LimitsConfig struct {
	MaxFileSize          int64 `yaml:"max-file-size"` // bytes
	MaxConcurrentUploads int   `yaml:"max-concurrent-uploads"`
	RateLimit            int   `yaml:"rate-limit"` // requests per minute
}

type StorageConfig struct {
	Type   string            `yaml:"type"` // local, s3
	Config map[string]string `yaml:"config"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
