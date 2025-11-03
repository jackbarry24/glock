package glock

import (
	"testing"
	"time"
)

func TestConnect(t *testing.T) {
	tests := []struct {
		name        string
		serverURL   string
		expectError bool
	}{
		{"valid URL", "http://localhost:8080", false},
		{"empty URL", "", true},
		{"valid HTTPS URL", "https://example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Connect(tt.serverURL)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if client.ServerURL != tt.serverURL {
					t.Errorf("expected server URL %s, got %s", tt.serverURL, client.ServerURL)
				}
				if client.ID == "" {
					t.Error("client ID should not be empty")
				}
			}
		})
	}
}

func TestConnectWithConfig(t *testing.T) {
	config := &ClientConfig{ServerURL: "http://test-server:8080", Timeout: 10 * time.Second, MaxRetries: 5}
	client, err := ConnectWithConfig(config)
	if err != nil {
		t.Fatalf("failed to connect with config: %v", err)
	}
	if client.ServerURL != config.ServerURL {
		t.Errorf("expected server URL %s, got %s", config.ServerURL, client.ServerURL)
	}
}

func TestDefaultClientConfig(t *testing.T) {
	serverURL := "http://test-server:8080"
	config := DefaultClientConfig(serverURL)
	if config.ServerURL != serverURL {
		t.Errorf("expected server URL %s, got %s", serverURL, config.ServerURL)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", config.Timeout)
	}
	if config.MaxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", config.MaxRetries)
	}
	if config.HTTPClient == nil {
		t.Error("HTTP client should not be nil")
	}
}
