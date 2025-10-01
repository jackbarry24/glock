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

	Capacity            int           `yaml:"capacity" json:"capacity" example:"1000"`
	DefaultTTL          time.Duration `yaml:"default_ttl" json:"default_ttl" swaggertype:"string" example:"30s"`
	DefaultMaxTTL       time.Duration `yaml:"default_max_ttl" json:"default_max_ttl" swaggertype:"string" example:"5m"`
	DefaultQueueTimeout time.Duration `yaml:"default_queue_timeout" json:"default_queue_timeout" swaggertype:"string" example:"5m"`

	OwnerHistoryMaxSize int `yaml:"owner_history_max_size" json:"owner_history_max_size" example:"100"`

	QueueMaxSize int `yaml:"queue_max_size" json:"queue_max_size" example:"1024"`

	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" swaggertype:"string" example:"30s"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Port:                8080,
		Host:                "",
		Capacity:            1000,
		DefaultTTL:          30 * time.Second,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultQueueTimeout: 5 * time.Minute,
		OwnerHistoryMaxSize: 100,
		QueueMaxSize:        1024,

		CleanupInterval: 5 * time.Second,
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

	if queueTimeout := os.Getenv("GLOCK_DEFAULT_QUEUE_TIMEOUT"); queueTimeout != "" {
		if d, err := time.ParseDuration(queueTimeout); err == nil && d > 0 {
			c.DefaultQueueTimeout = d
		}
	}

	if cleanupInterval := os.Getenv("GLOCK_CLEANUP_INTERVAL"); cleanupInterval != "" {
		if d, err := time.ParseDuration(cleanupInterval); err == nil && d > 0 {
			c.CleanupInterval = d
		}
	}

	if ownerHistoryMaxSize := os.Getenv("GLOCK_OWNER_HISTORY_MAX_SIZE"); ownerHistoryMaxSize != "" {
		if size, err := strconv.Atoi(ownerHistoryMaxSize); err == nil && size > 0 {
			c.OwnerHistoryMaxSize = size
		}
	}

	if queueMaxSize := os.Getenv("GLOCK_QUEUE_MAX_SIZE"); queueMaxSize != "" {
		if size, err := strconv.Atoi(queueMaxSize); err == nil && size > 0 {
			c.QueueMaxSize = size
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

	if c.CleanupInterval <= 0 {
		return fmt.Errorf("cleanup_interval must be greater than 0")
	}

	if c.OwnerHistoryMaxSize <= 0 {
		return fmt.Errorf("owner_history_max_size must be greater than 0")
	}

	if c.QueueMaxSize <= 0 {
		return fmt.Errorf("queue_max_size must be greater than 0")
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
