package kubehubcli

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   string `yaml:"server"`
	Issuer   string `yaml:"issuer"`
	ClientID string `yaml:"client_id"`
}

func (c *Config) setDefaults() {
	if c.Issuer == "" {
		c.Issuer = "https://login.kubehub.io/realms/Kubehub"
	}
	if c.ClientID == "" {
		c.ClientID = "publicClient"
	}
	if c.Server == "" {
		c.Server = "https://api.kubehub.io"
	}
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "kubehubcli", "config.yaml"), nil
}

func LoadConfig() (*Config, error) {
	cfg := &Config{}
	cfg.setDefaults()

	path, err := DefaultConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	cfg.setDefaults()
	return cfg, nil
}
