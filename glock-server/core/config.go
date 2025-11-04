package core

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the glock server
// @Description Config holds all configuration for the glock server
type Config struct {
	Port int    `yaml:"port" json:"port" example:"8080"`
	Host string `yaml:"host" json:"host" example:"localhost"`

	NumShards            int           `yaml:"num_shards" json:"num_shards" example:"32"`
	Capacity             int           `yaml:"capacity" json:"capacity" example:"10000"`
	DefaultTTL           time.Duration `yaml:"default_ttl" json:"default_ttl" swaggertype:"string" example:"30s"`
	DefaultMaxTTL        time.Duration `yaml:"default_max_ttl" json:"default_max_ttl" swaggertype:"string" example:"5m"`
	DefaultQueueTimeout  time.Duration `yaml:"default_queue_timeout" json:"default_queue_timeout" swaggertype:"string" example:"5m"`
	DefaultQueueCapacity int           `yaml:"default_queue_capacity" json:"default_queue_capacity" example:"1024"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Port:                 8080,
		Host:                 "",
		Capacity:             10000,
		DefaultTTL:           30 * time.Second,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultQueueTimeout:  5 * time.Minute,
		DefaultQueueCapacity: 1024,
	}
}

// LoadFromEnv loads configuration from environment variables
func (c *Config) LoadFromEnv() error {
	if port := os.Getenv("GLOCK_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Port = p
		}
	}

	if host := os.Getenv("GLOCK_HOST"); host != "" {
		c.Host = host
	}

	if capacity := os.Getenv("GLOCK_CAPACITY"); capacity != "" {
		if cap, err := strconv.Atoi(capacity); err == nil && cap > 0 {
			c.Capacity = cap
		}
	}

	if ttl := os.Getenv("GLOCK_DEFAULT_TTL"); ttl != "" {
		if d, err := time.ParseDuration(ttl); err == nil && d > 0 {
			c.DefaultTTL = d
		}
	}

	if maxTTL := os.Getenv("GLOCK_DEFAULT_MAX_TTL"); maxTTL != "" {
		if d, err := time.ParseDuration(maxTTL); err == nil && d > 0 {
			c.DefaultMaxTTL = d
		}
	}

	if defaultQueueTimeout := os.Getenv("GLOCK_DEFAULT_QUEUE_TIMEOUT"); defaultQueueTimeout != "" {
		if d, err := time.ParseDuration(defaultQueueTimeout); err == nil && d > 0 {
			c.DefaultQueueTimeout = d
		}
	}

	if defaultQueueCapacity := os.Getenv("GLOCK_DEFAULT_QUEUE_CAPACITY"); defaultQueueCapacity != "" {
		if capacity, err := strconv.Atoi(defaultQueueCapacity); err == nil && capacity > 0 {
			c.DefaultQueueCapacity = capacity
		}
	}

	return nil
}

// LoadFromYAML loads configuration from a YAML file
func (c *Config) LoadFromYAML(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse config file %s: %w", filename, err)
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	if c.Capacity < 1 {
		return fmt.Errorf("capacity must be greater than 0")
	}

	if c.Capacity > 1048576 {
		return fmt.Errorf("capacity must be less than or equal to 1048576 (2^20)")
	}

	if c.DefaultTTL <= 0 {
		return fmt.Errorf("default_ttl must be greater than 0")
	}

	if c.DefaultMaxTTL <= 0 {
		return fmt.Errorf("default_max_ttl must be greater than 0")
	}

	if c.DefaultMaxTTL < c.DefaultTTL {
		return fmt.Errorf("default_max_ttl must be greater than or equal to default_ttl")
	}

	if c.DefaultQueueTimeout <= 0 {
		return fmt.Errorf("default_queue_timeout must be greater than 0")
	}

	if c.DefaultQueueCapacity <= 0 {
		return fmt.Errorf("default_queue_capacity must be greater than 0")
	}

	if c.DefaultQueueCapacity > 1048576 {
		return fmt.Errorf("default_queue_capacity must be less than or equal to 1048576 (2^20)")
	}

	return nil
}

// LoadConfig loads configuration from multiple sources with precedence:
// 1. Default values
// 2. YAML file (if provided)
// 3. Environment variables (overrides YAML)
// Returns the loaded configuration and any error
func LoadConfig(yamlFile string) (*Config, error) {
	config := DefaultConfig()

	if yamlFile != "" {
		if err := config.LoadFromYAML(yamlFile); err != nil {
			return nil, err
		}
	}

	if err := config.LoadFromEnv(); err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}
