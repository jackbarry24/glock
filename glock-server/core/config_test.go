package core

import (
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", config.Port)
	}
	if config.Capacity != 1000 {
		t.Errorf("Expected capacity 1000, got %d", config.Capacity)
	}
	if config.DefaultTTL != 30*time.Second {
		t.Errorf("Expected default TTL 30s, got %v", config.DefaultTTL)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Port:                8080,
				Capacity:            100,
				DefaultTTL:          time.Second,
				DefaultMaxTTL:       5 * time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid port - too low",
			config: Config{
				Port:                0,
				Capacity:            100,
				DefaultTTL:          time.Second,
				DefaultMaxTTL:       5 * time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid port - too high",
			config: Config{
				Port:                70000,
				Capacity:            100,
				DefaultTTL:          time.Second,
				DefaultMaxTTL:       5 * time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid capacity",
			config: Config{
				Port:                8080,
				Capacity:            0,
				DefaultTTL:          time.Second,
				DefaultMaxTTL:       5 * time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid default TTL",
			config: Config{
				Port:                8080,
				Capacity:            100,
				DefaultTTL:          0,
				DefaultMaxTTL:       5 * time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: true,
		},
		{
			name: "max TTL less than TTL",
			config: Config{
				Port:                8080,
				Capacity:            100,
				DefaultTTL:          5 * time.Minute,
				DefaultMaxTTL:       time.Minute,
				DefaultQueueTimeout: time.Minute,
				CleanupInterval:     time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("GLOCK_PORT", "9090")
	os.Setenv("GLOCK_CAPACITY", "500")
	os.Setenv("GLOCK_DEFAULT_TTL", "1m")
	os.Setenv("GLOCK_METRICS_ENABLED", "false")
	defer func() {
		// Clean up
		os.Unsetenv("GLOCK_PORT")
		os.Unsetenv("GLOCK_CAPACITY")
		os.Unsetenv("GLOCK_DEFAULT_TTL")
		os.Unsetenv("GLOCK_METRICS_ENABLED")
	}()

	config := DefaultConfig()
	err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if config.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", config.Port)
	}
	if config.Capacity != 500 {
		t.Errorf("Expected capacity 500, got %d", config.Capacity)
	}
	if config.DefaultTTL != time.Minute {
		t.Errorf("Expected default TTL 1m, got %v", config.DefaultTTL)
	}
}

func TestLoadConfigFromYAML(t *testing.T) {
	yamlContent := `
port: 7070
capacity: 200
default_ttl: 45s
`

	// Create temporary file
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config := DefaultConfig()
	err = config.LoadFromYAML(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadFromYAML() error = %v", err)
	}

	if config.Port != 7070 {
		t.Errorf("Expected port 7070, got %d", config.Port)
	}
	if config.Capacity != 200 {
		t.Errorf("Expected capacity 200, got %d", config.Capacity)
	}
	if config.DefaultTTL != 45*time.Second {
		t.Errorf("Expected default TTL 45s, got %v", config.DefaultTTL)
	}
}

func TestLoadConfig(t *testing.T) {
	// Test loading default config
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", config.Port)
	}

	// Test with invalid YAML file
	_, err = LoadConfig("/nonexistent/file.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}
