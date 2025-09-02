package glock

import (
	"testing"
	"time"
)

// TestConnect tests client connection functionality
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

// TestConnectWithConfig tests client connection with custom configuration
func TestConnectWithConfig(t *testing.T) {
	config := &ClientConfig{
		ServerURL:  "http://test-server:8080",
		Timeout:    10 * time.Second,
		MaxRetries: 5,
	}

	client, err := ConnectWithConfig(config)
	if err != nil {
		t.Fatalf("failed to connect with config: %v", err)
	}

	if client.ServerURL != config.ServerURL {
		t.Errorf("expected server URL %s, got %s", config.ServerURL, client.ServerURL)
	}
}

// TestDefaultClientConfig tests default client configuration
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

// TestGetOwnedLocks tests retrieving owned locks
func TestGetOwnedLocks(t *testing.T) {
	client, _ := Connect("http://test-server")

	// Add some locks to the client's store
	lock1 := &Lock{Name: "lock1", Owner: "owner1"}
	lock2 := &Lock{Name: "lock2", Owner: "owner2"}
	client.Locks.Store("lock1", lock1)
	client.Locks.Store("lock2", lock2)

	ownedLocks := client.GetOwnedLocks()
	if len(ownedLocks) != 2 {
		t.Errorf("expected 2 owned locks, got %d", len(ownedLocks))
	}

	// Check that both locks are present
	lockNames := make(map[string]bool)
	for _, lock := range ownedLocks {
		lockNames[lock.Name] = true
	}

	if !lockNames["lock1"] || !lockNames["lock2"] {
		t.Error("expected both lock1 and lock2 to be present")
	}
}

// TestGetLockByName tests retrieving a lock by name
func TestGetLockByName(t *testing.T) {
	client, _ := Connect("http://test-server")

	// Add a lock to the client's store
	lock := &Lock{Name: "test-lock", Owner: "test-owner"}
	client.Locks.Store("test-lock", lock)

	// Test retrieving existing lock
	retrievedLock, exists := client.GetLockByName("test-lock")
	if !exists {
		t.Error("expected lock to exist")
	}
	if retrievedLock.Name != "test-lock" {
		t.Errorf("expected lock name 'test-lock', got '%s'", retrievedLock.Name)
	}

	// Test retrieving non-existent lock
	_, exists = client.GetLockByName("non-existent")
	if exists {
		t.Error("expected non-existent lock to not exist")
	}
}

// TestLockIsExpired tests lock expiry checking
func TestLockIsExpired(t *testing.T) {
	lock := &Lock{
		Name: "test-lock",
		TTL:  30 * time.Second,
	}

	// For now, IsExpired always returns false
	if lock.IsExpired() {
		t.Error("expected lock to not be expired")
	}
}

// TestLockTimeUntilExpiry tests time until expiry calculation
func TestLockTimeUntilExpiry(t *testing.T) {
	lock := &Lock{
		Name: "test-lock",
		TTL:  30 * time.Second,
	}

	timeUntilExpiry := lock.TimeUntilExpiry()
	if timeUntilExpiry != 30*time.Second {
		t.Errorf("expected time until expiry to be 30s, got %v", timeUntilExpiry)
	}
}

// TestLockString tests lock string representation
func TestLockString(t *testing.T) {
	lock := &Lock{
		Name:  "test-lock",
		Owner: "test-owner",
		Token: 12345,
		TTL:   30 * time.Second,
	}

	str := lock.String()
	expected := "Lock{Name: test-lock, Owner: test-owner, Token: 12345, TTL: 30s}"
	if str != expected {
		t.Errorf("expected string '%s', got '%s'", expected, str)
	}
}

// TestQueueBehaviorConstants tests queue behavior constants
func TestQueueBehaviorConstants(t *testing.T) {
	if QueueNone != "none" {
		t.Errorf("expected QueueNone to be 'none', got '%s'", QueueNone)
	}
	if QueueFIFO != "fifo" {
		t.Errorf("expected QueueFIFO to be 'fifo', got '%s'", QueueFIFO)
	}
	if QueueLIFO != "lifo" {
		t.Errorf("expected QueueLIFO to be 'lifo', got '%s'", QueueLIFO)
	}
}
