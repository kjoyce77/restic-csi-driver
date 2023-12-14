package config

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Secret represents the structure of the secrets file
type Secret map[string]string

// Volume Information
type VolumeInformation struct {
	StagingPath  string `toml:"staging_path"`
	ThinPoolName string `toml:"thin_pool_name"`
}

// Destination represents a Restic repository destination
type Destination struct {
	Environment map[string]string `toml:"environment"`
	Repository  string            `toml:"repo"`
}

// Config represents the configuration structure
type Config struct {
	VolumeInformation VolumeInformation `toml:"volume_info"`
	ResticRepo        []Destination     `toml:"restic_repo"`
}

func LoadConfig(configFilePath, secretFilePath string) (Config, error) {
	var config Config
	var secret Secret

	// Load and parse the secret file
	secretData, err := os.ReadFile(secretFilePath)
	if err != nil {
		return config, err
	}
	if _, err := toml.Decode(string(secretData), &secret); err != nil {
		return config, err
	}

	// Load and parse the configuration file
	configData, err := os.ReadFile(configFilePath)
	if err != nil {
		return config, err
	}
	if _, err := toml.Decode(string(configData), &config); err != nil {
		return config, err
	}

	// Replace 'secret:' placeholders with actual values
	for i, repo := range config.ResticRepo {
		for key, val := range repo.Environment {
			if strings.HasPrefix(val, "secret:") {
				secretKey := val[7:] // Remove 'secret:' prefix
				if secretVal, ok := secret[secretKey]; ok {
					config.ResticRepo[i].Environment[key] = secretVal
				}
			}
		}
	}

	return config, nil
}
