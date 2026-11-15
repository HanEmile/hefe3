package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server     ServerConfig     `toml:"server"`
	Database   DatabaseConfig   `toml:"database"`
	Federation FederationConfig `toml:"federation"`
	Media      MediaConfig      `toml:"media"`
	SFU        SFUConfig        `toml:"sfu"`
}

type ServerConfig struct {
	Address  string `toml:"address"`
	Hostname string `toml:"hostname"`
}

type DatabaseConfig struct {
	Driver string `toml:"driver"`
	DSN    string `toml:"dsn"`
}

type FederationConfig struct {
	Enabled bool   `toml:"enabled"`
	Address string `toml:"address"`
	TLSCert string `toml:"tls_cert"`
	TLSKey  string `toml:"tls_key"`
}

type MediaConfig struct {
	UploadDir string `toml:"upload_dir"`
	MaxSize   int64  `toml:"max_size"`
}

type SFUConfig struct {
	Enabled    bool     `toml:"enabled"`
	ICEServers []string `toml:"ice_servers"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)
	return &cfg, nil
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Address:  ":8080",
			Hostname: "localhost",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "xilebo.db",
		},
		Federation: FederationConfig{
			Address: ":8448",
		},
		Media: MediaConfig{
			UploadDir: "uploads",
			MaxSize:   50 * 1024 * 1024, // 50MB
		},
		SFU: SFUConfig{
			Enabled:    true,
			ICEServers: []string{"stun:stun.l.google.com:19302"},
		},
	}
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("XILEBO_ADDRESS"); v != "" {
		cfg.Server.Address = v
	}
	if v := os.Getenv("XILEBO_HOSTNAME"); v != "" {
		cfg.Server.Hostname = v
	}
	if v := os.Getenv("XILEBO_DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("XILEBO_DB_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("XILEBO_FEDERATION_ENABLED"); v == "true" {
		cfg.Federation.Enabled = true
	}
	if v := os.Getenv("XILEBO_MEDIA_DIR"); v != "" {
		cfg.Media.UploadDir = v
	}
}
