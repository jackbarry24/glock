package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Port != 8080 {
		t.Errorf("Default port = %d, want 8080", cfg.Port)
	}

	if cfg.DefaultTTL != 30*time.Second {
		t.Errorf("Default TTL = %v, want 30s", cfg.DefaultTTL)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Default config should be valid: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "invalid port - too low",
			modify:  func(c *Config) { c.Port = 0 },
			wantErr: true,
		},
		{
			name:    "invalid port - too high",
			modify:  func(c *Config) { c.Port = 99999 },
			wantErr: true,
		},
		{
			name:    "invalid capacity - zero",
			modify:  func(c *Config) { c.Capacity = 0 },
			wantErr: true,
		},
		{
			name:    "invalid capacity - exceeds max",
			modify:  func(c *Config) { c.Capacity = 1048577 },
			wantErr: true,
		},
		{
			name:    "invalid default ttl",
			modify:  func(c *Config) { c.DefaultTTL = 0 },
			wantErr: true,
		},
		{
			name:    "invalid max ttl",
			modify:  func(c *Config) { c.DefaultMaxTTL = 0 },
			wantErr: true,
		},
		{
			name: "max ttl less than default ttl",
			modify: func(c *Config) {
				c.DefaultTTL = 60 * time.Second
				c.DefaultMaxTTL = 30 * time.Second
			},
			wantErr: true,
		},
		{
			name:    "invalid queue timeout",
			modify:  func(c *Config) { c.DefaultQueueTimeout = 0 },
			wantErr: true,
		},
		{
			name:    "invalid queue max size - zero",
			modify:  func(c *Config) { c.DefaultQueueCapacity = 0 },
			wantErr: true,
		},
		{
			name:    "invalid queue max size - exceeds max",
			modify:  func(c *Config) { c.DefaultQueueCapacity = 1048577 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigLoadFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(*testing.T, *Config)
	}{
		{
			name: "load port",
			envVars: map[string]string{
				"GLOCK_PORT": "9090",
			},
			validate: func(t *testing.T, c *Config) {
				if c.Port != 9090 {
					t.Errorf("Port = %d, want 9090", c.Port)
				}
			},
		},
		{
			name: "load host",
			envVars: map[string]string{
				"GLOCK_HOST": "0.0.0.0",
			},
			validate: func(t *testing.T, c *Config) {
				if c.Host != "0.0.0.0" {
					t.Errorf("Host = %s, want 0.0.0.0", c.Host)
				}
			},
		},
		{
			name: "load ttl",
			envVars: map[string]string{
				"GLOCK_DEFAULT_TTL": "1m",
			},
			validate: func(t *testing.T, c *Config) {
				if c.DefaultTTL != time.Minute {
					t.Errorf("DefaultTTL = %v, want 1m", c.DefaultTTL)
				}
			},
		},
		{
			name: "load capacity",
			envVars: map[string]string{
				"GLOCK_CAPACITY": "5000",
			},
			validate: func(t *testing.T, c *Config) {
				if c.Capacity != 5000 {
					t.Errorf("Capacity = %d, want 5000", c.Capacity)
				}
			},
		},
		{
			name: "load queue timeout",
			envVars: map[string]string{
				"GLOCK_DEFAULT_QUEUE_TIMEOUT": "10m",
			},
			validate: func(t *testing.T, c *Config) {
				if c.DefaultQueueTimeout != 10*time.Minute {
					t.Errorf("DefaultQueueTimeout = %v, want 10m", c.DefaultQueueTimeout)
				}
			},
		},
		{
			name: "load default queue capacity",
			envVars: map[string]string{
				"GLOCK_DEFAULT_QUEUE_CAPACITY": "2048",
			},
			validate: func(t *testing.T, c *Config) {
				if c.DefaultQueueCapacity != 2048 {
					t.Errorf("DefaultQueueCapacity = %d, want 2048", c.DefaultQueueCapacity)
				}
			},
		},
		{
			name: "invalid port ignored",
			envVars: map[string]string{
				"GLOCK_PORT": "not-a-number",
			},
			validate: func(t *testing.T, c *Config) {
				if c.Port != 8080 {
					t.Errorf("Port should remain default 8080, got %d", c.Port)
				}
			},
		},
		{
			name: "invalid duration ignored",
			envVars: map[string]string{
				"GLOCK_DEFAULT_TTL": "invalid",
			},
			validate: func(t *testing.T, c *Config) {
				if c.DefaultTTL != 30*time.Second {
					t.Errorf("DefaultTTL should remain default, got %v", c.DefaultTTL)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			cfg := DefaultConfig()
			if err := cfg.LoadFromEnv(); err != nil {
				t.Fatalf("LoadFromEnv() error = %v", err)
			}

			tt.validate(t, cfg)
		})
	}
}

func TestConfigLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		yaml     string
		wantErr  bool
		validate func(*testing.T, *Config)
	}{
		{
			name: "valid yaml",
			yaml: `
port: 9000
host: "127.0.0.1"
capacity: 5000
default_queue_capacity: 2048
default_ttl: 45s
default_max_ttl: 10m
default_queue_timeout: 10m
`,
			wantErr: false,
			validate: func(t *testing.T, c *Config) {
				if c.Port != 9000 {
					t.Errorf("Port = %d, want 9000", c.Port)
				}
				if c.Host != "127.0.0.1" {
					t.Errorf("Host = %s, want 127.0.0.1", c.Host)
				}
				if c.Capacity != 5000 {
					t.Errorf("Capacity = %d, want 5000", c.Capacity)
				}
				if c.DefaultQueueCapacity != 2048 {
					t.Errorf("DefaultQueueCapacity = %d, want 2048", c.DefaultQueueCapacity)
				}
				if c.DefaultQueueTimeout != 10*time.Minute {
					t.Errorf("DefaultQueueTimeout = %v, want 10m", c.DefaultQueueTimeout)
				}
			},
		},
		{
			name: "invalid yaml",
			yaml: `
port: [invalid
`,
			wantErr: true,
		},
		{
			name:    "empty file",
			yaml:    "",
			wantErr: false,
			validate: func(t *testing.T, c *Config) {
				if c.Port != 8080 {
					t.Errorf("Port should remain default 8080 for empty yaml, got %d", c.Port)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(filename, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cfg := DefaultConfig()
			err := cfg.LoadFromYAML(filename)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromYAML() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("no file specified", func(t *testing.T) {
		cfg, err := LoadConfig("")
		if err != nil {
			t.Fatalf("LoadConfig(\"\") error = %v", err)
		}
		if cfg.Port != 8080 {
			t.Errorf("Should have default port 8080, got %d", cfg.Port)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := LoadConfig("/nonexistent/file.yaml")
		if err == nil {
			t.Error("LoadConfig() should error for nonexistent file")
		}
	})

	t.Run("yaml and env precedence", func(t *testing.T) {
		tmpDir := t.TempDir()
		filename := filepath.Join(tmpDir, "config.yaml")
		yaml := `
port: 9000
capacity: 5000
default_queue_capacity: 2048
default_queue_timeout: 10m
`
		if err := os.WriteFile(filename, []byte(yaml), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		os.Setenv("GLOCK_PORT", "9090")
		defer os.Unsetenv("GLOCK_PORT")

		cfg, err := LoadConfig(filename)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if cfg.Port != 9090 {
			t.Errorf("Env should override YAML: Port = %d, want 9090", cfg.Port)
		}

		if cfg.Capacity != 5000 {
			t.Errorf("YAML value should be used: Capacity = %d, want 5000", cfg.Capacity)
		}

		if cfg.DefaultQueueCapacity != 2048 {
			t.Errorf("YAML value should be used: QueueMaxSize = %d, want 2048", cfg.DefaultQueueCapacity)
		}

		if cfg.DefaultQueueTimeout != 10*time.Minute {
			t.Errorf("YAML value should be used: DefaultQueueTimeout = %v, want 10m", cfg.DefaultQueueTimeout)
		}
	})
}
