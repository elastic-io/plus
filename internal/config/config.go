package config

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen       string                `yaml:"listen"`
	StoragePath  string                `yaml:"storage_path"`
	DatabasePath string                `yaml:"database_path"`
	Debug        bool                  `yaml:"debug"`
	Auth         AuthConfig            `yaml:"auth"`
	Cache        CacheConfig           `yaml:"cache"`
	Repositories map[string]RepoConfig `yaml:"repositories"`
	Limits       LimitsConfig          `yaml:"limits"`
	Storage      StorageConfig         `yaml:"storage"`
	DevMode      bool                  `yaml:"dev_mode"`
}

type AuthConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Token           string `yaml:"token"`
	APIKey          string `yaml:"api_key"`
	RequireReadAuth bool   `yaml:"require_read_auth"`
}

type CacheConfig struct {
	Enabled bool   `yaml:"enabled"`
	TTL     string `yaml:"ttl"`
	MaxSize int    `yaml:"max_size"`
}

type RepoConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"` // rpm, deb
	Enabled     bool   `yaml:"enabled"`
	AutoRefresh bool   `yaml:"auto_refresh"`
}

type LimitsConfig struct {
	MaxFileSize          int64 `yaml:"max_file_size"` // bytes
	MaxConcurrentUploads int   `yaml:"max_concurrent_uploads"`
	RateLimit            int   `yaml:"rate_limit"` // requests per minute
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
