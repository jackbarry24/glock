package glock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Lock represents a lock acquired from the glock-server
// It provides methods for heartbeating and releasing the lock.
type Lock struct {
	Name        string        `json:"name"`
	Owner       string        `json:"owner"`
	OwnerID     string        `json:"owner_id"`
	Token       uint          `json:"token"`
	TTL         time.Duration `json:"ttl"`
	Frozen      bool          `json:"frozen"`
	serverURL   string        `json:"-"`
	heartbeatCh chan struct{} `json:"-"`
}

type Glock struct {
	ServerURL  string
	ID         string
	Locks      sync.Map
	httpClient *http.Client
}

// ClientConfig holds configuration options for the client
type ClientConfig struct {
	ServerURL  string
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
	HTTPClient *http.Client
}

// DefaultClientConfig returns a default client configuration
func DefaultClientConfig(serverURL string) *ClientConfig {
	return &ClientConfig{
		ServerURL:  serverURL,
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// getHTTPClient returns the configured HTTP client or the default one
func (g *Glock) getHTTPClient() *http.Client {
	if g.httpClient != nil {
		return g.httpClient
	}
	return http.DefaultClient
}

// PollRequest for checking queue status
type PollRequest struct {
	RequestID string `json:"request_id"`
	OwnerID   string `json:"owner_id"`
	Name      string `json:"name"`
}

// Connect creates a new Glock client with default configuration
func Connect(serverURL string) (*Glock, error) {
	config := DefaultClientConfig(serverURL)
	return ConnectWithConfig(config)
}

// ConnectWithConfig creates a new Glock client with custom configuration
func ConnectWithConfig(config *ClientConfig) (*Glock, error) {
	if config.ServerURL == "" {
		return nil, fmt.Errorf("server URL must not be empty")
	}

	return &Glock{
		ServerURL:  config.ServerURL,
		ID:         uuid.NewString(),
		httpClient: config.HTTPClient,
	}, nil
}

// GetOwnedLocks returns all locks currently owned by this client
func (g *Glock) GetOwnedLocks() []*Lock {
	var ownedLocks []*Lock
	g.Locks.Range(func(key, value interface{}) bool {
		if lock, ok := value.(*Lock); ok {
			ownedLocks = append(ownedLocks, lock)
		}
		return true
	})
	return ownedLocks
}

// ReleaseAllLocks releases all locks owned by this client
func (g *Glock) ReleaseAllLocks() []error {
	var errors []error
	g.Locks.Range(func(key, value interface{}) bool {
		if lock, ok := value.(*Lock); ok {
			// Check if we still own the lock before trying to release it
			if owns, err := lock.VerifyOwnership(); err == nil && owns {
				if err := lock.Release(); err != nil {
					errors = append(errors, fmt.Errorf("failed to release lock %s: %v", lock.Name, err))
				}
			}
		}
		return true
	})
	return errors
}

// GetLockByName retrieves a lock from the client's local cache
func (g *Glock) GetLockByName(name string) (*Lock, bool) {
	if value, exists := g.Locks.Load(name); exists {
		if lock, ok := value.(*Lock); ok {
			return lock, true
		}
	}
	return nil, false
}

// Acquire requests a lock from the glock-server and returns a Lock instance
func (g *Glock) Acquire(lockName, owner string) (*Lock, error) {
	queueRequest := false
	acquireReq := AcquireRequest{
		Name:         lockName,
		Owner:        owner,
		OwnerID:      g.ID,
		QueueRequest: &queueRequest,
	}
	body, err := json.Marshal(acquireReq)
	if err != nil {
		return nil, err
	}
	resp, err := g.getHTTPClient().Post(g.ServerURL+"/api/acquire", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	acquireResp := AcquireResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&acquireResp); err != nil {
		return nil, err
	}
	if acquireResp.Lock == nil {
		return nil, fmt.Errorf("no lock returned from server")
	}
	serverLock := acquireResp.Lock
	l := &Lock{
		Name:        serverLock.Name,
		Owner:       serverLock.Owner,
		OwnerID:     serverLock.OwnerID,
		Token:       serverLock.Token,
		TTL:         serverLock.TTL,
		serverURL:   g.ServerURL,
		heartbeatCh: make(chan struct{}),
	}
	g.Locks.Store(l.Name, l)
	return l, nil
}

// CreateLock creates a new lock with queue configuration
func (g *Glock) CreateLock(name string, ttl, maxTTL string, queueType QueueBehavior, queueTimeout string) error {
	req := CreateRequest{
		Name:         name,
		TTL:          ttl,
		MaxTTL:       maxTTL,
		QueueType:    queueType,
		QueueTimeout: queueTimeout,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(g.ServerURL+"/api/create", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create lock: %s", resp.Status)
	}

	return nil
}

// UpdateLock updates an existing lock's configuration
func (g *Glock) UpdateLock(name string, ttl, maxTTL string, queueType QueueBehavior, queueTimeout string, metadata any) error {
	req := UpdateRequest{
		Name:         name,
		TTL:          ttl,
		MaxTTL:       maxTTL,
		QueueType:    queueType,
		QueueTimeout: queueTimeout,
		Metadata:     metadata,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(g.ServerURL+"/api/update", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update lock: %s", resp.Status)
	}

	return nil
}

// CreateLockWithMetadata creates a new lock with full configuration including metadata
func (g *Glock) CreateLockWithMetadata(name string, ttl, maxTTL string, queueType QueueBehavior, queueTimeout string, metadata any) error {
	req := CreateRequest{
		Name:         name,
		TTL:          ttl,
		MaxTTL:       maxTTL,
		QueueType:    queueType,
		QueueTimeout: queueTimeout,
		Metadata:     metadata,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(g.ServerURL+"/api/create", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create lock: %s", resp.Status)
	}

	return nil
}

// AcquireOrQueue attempts to acquire a lock, returning either the lock or queue info
func (g *Glock) AcquireOrQueue(lockName, owner string) (*Lock, *QueueResponse, error) {
	queueRequest := true
	acquireReq := AcquireRequest{
		Name:         lockName,
		Owner:        owner,
		OwnerID:      g.ID,
		QueueRequest: &queueRequest,
	}
	body, err := json.Marshal(acquireReq)
	if err != nil {
		return nil, nil, err
	}

	resp, err := http.Post(g.ServerURL+"/api/acquire", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// Handle different response types
	var responseBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		return nil, nil, err
	}

	if lockData, ok := responseBody["lock"]; ok {
		// Lock was acquired immediately
		lockBytes, _ := json.Marshal(lockData)
		var lock Lock
		if err := json.Unmarshal(lockBytes, &lock); err != nil {
			return nil, nil, err
		}
		lock.serverURL = g.ServerURL
		lock.heartbeatCh = make(chan struct{})
		g.Locks.Store(lock.Name, &lock)
		return &lock, nil, nil
	}

	if queueData, ok := responseBody["queue"]; ok {
		// Lock was queued
		queueBytes, _ := json.Marshal(queueData)
		var queueResp QueueResponse
		if err := json.Unmarshal(queueBytes, &queueResp); err != nil {
			return nil, nil, err
		}
		return nil, &queueResp, nil
	}

	return nil, nil, fmt.Errorf("unexpected response format")
}

// DeleteLock deletes an existing lock
func (g *Glock) DeleteLock(name string) error {
	req, err := http.NewRequest("DELETE", g.ServerURL+"/api/delete/"+name, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete lock: %s", resp.Status)
	}

	return nil
}

// ListLocks returns a list of all lock names on the server
func (g *Glock) ListLocks() ([]string, error) {
	resp, err := http.Get(g.ServerURL + "/api/list")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Locks []*string `json:"locks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var lockNames []string
	for _, lockPtr := range result.Locks {
		if lockPtr != nil {
			lockNames = append(lockNames, *lockPtr)
		}
	}
	return lockNames, nil
}

// GetServerStatus returns the current server status
func (g *Glock) GetServerStatus() (map[string]interface{}, error) {
	resp, err := http.Get(g.ServerURL + "/api/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// HealthCheck verifies server connectivity and returns server information
func (g *Glock) HealthCheck() error {
	resp, err := http.Get(g.ServerURL + "/api/status")
	if err != nil {
		return fmt.Errorf("server health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status: %s", resp.Status)
	}

	return nil
}

// PollQueue checks the status of a queued lock request
func (g *Glock) PollQueue(lockName, requestID string) (*PollResponse, error) {
	pollReq := PollRequest{
		Name:      lockName,
		RequestID: requestID,
		OwnerID:   g.ID,
	}

	body, err := json.Marshal(pollReq)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(g.ServerURL+"/api/poll", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pollResp PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
		return nil, err
	}

	// If lock was acquired, set up the lock
	if pollResp.Lock != nil {
		pollResp.Lock.serverURL = g.ServerURL
		pollResp.Lock.heartbeatCh = make(chan struct{})
		g.Locks.Store(pollResp.Lock.Name, pollResp.Lock)
	}

	return &pollResp, nil
}

// RemoveFromQueue removes a queued lock request
func (g *Glock) RemoveFromQueue(lockName, requestID string) error {
	req := PollRequest{
		Name:      lockName,
		RequestID: requestID,
		OwnerID:   g.ID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(g.ServerURL+"/api/remove", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return fmt.Errorf("server error: %s", errorMsg)
		}
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

// AcquireOrWait attempts to acquire a lock, waiting up to the specified timeout if queued
func (g *Glock) AcquireOrWait(lockName, owner string, timeout time.Duration) (*Lock, error) {
	// First try to acquire or queue
	lock, queueResp, err := g.AcquireOrQueue(lockName, owner)
	if err != nil {
		return nil, err
	}

	// If we got the lock immediately, return it
	if lock != nil {
		return lock, nil
	}

	// If we got a queue response, poll until timeout
	if queueResp != nil {
		startTime := time.Now()
		pollInterval := 100 * time.Millisecond // Poll every 100ms

		for {
			// Check if we've exceeded the timeout
			if time.Since(startTime) > timeout {
				// Timeout reached, remove from queue
				if removeErr := g.RemoveFromQueue(lockName, queueResp.RequestID); removeErr != nil {
					fmt.Printf("Warning: failed to remove expired queue request %s: %v\n", queueResp.RequestID, removeErr)
				}
				return nil, fmt.Errorf("timeout waiting for lock after %v", timeout)
			}

			// Poll for status
			pollResp, err := g.PollQueue(lockName, queueResp.RequestID)
			if err != nil {
				return nil, fmt.Errorf("failed to poll queue: %v", err)
			}

			switch pollResp.Status {
			case "ready":
				if pollResp.Lock == nil {
					return nil, fmt.Errorf("server indicated lock ready but no lock provided")
				}
				return pollResp.Lock, nil
			case "waiting":
				// Still waiting, sleep and try again
				time.Sleep(pollInterval)
			case "expired":
				// Our request expired
				return nil, fmt.Errorf("queue request expired")
			case "not_found":
				// Our request was removed or never existed
				return nil, fmt.Errorf("queue request not found")
			default:
				return nil, fmt.Errorf("unknown poll status: %s", pollResp.Status)
			}
		}
	}

	return nil, fmt.Errorf("unexpected response: no lock or queue information returned")
}

// StartHeartbeat starts a goroutine to refresh the lock before TTL expires
func (l *Lock) StartHeartbeat() {
	go func() {
		interval := time.Duration(float64(l.TTL) * 0.9)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshReq := RefreshRequest{
					Name:    l.Name,
					OwnerID: l.OwnerID,
					Token:   l.Token,
				}
				body, err := json.Marshal(refreshReq)
				if err != nil {
					fmt.Printf("Heartbeat marshal error for lock %s: %v\n", l.Name, err)
					continue
				}
				resp, err := http.Post(l.serverURL+"/api/refresh", "application/json", bytes.NewBuffer(body))
				if err != nil {
					fmt.Printf("Heartbeat error for lock %s: %v\n", l.Name, err)
					continue
				}
				// do not defer in loop to avoid accumulating defers
				if resp.StatusCode != http.StatusOK {
					fmt.Printf("Heartbeat error for lock %s: server returned status %d\n", l.Name, resp.StatusCode)
					_ = resp.Body.Close()
					continue
				}
				refreshResp := RefreshResponse{}
				if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
					fmt.Printf("Heartbeat decode error for lock %s: %v\n", l.Name, err)
					_ = resp.Body.Close()
					continue
				}
				_ = resp.Body.Close()
			case <-l.heartbeatCh:
				fmt.Printf("Heartbeat stopped for lock %s\n", l.Name)
				return
			}
		}
	}()
}

// VerifyOwnership checks if the client still owns this lock
func (l *Lock) VerifyOwnership() (bool, error) {
	verifyReq := VerifyRequest{
		Name:    l.Name,
		OwnerID: l.OwnerID,
		Token:   l.Token,
	}
	body, err := json.Marshal(verifyReq)
	if err != nil {
		return false, err
	}
	resp, err := http.Post(l.serverURL+"/api/verify", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return false, fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return false, fmt.Errorf("server error: %s", errorMsg)
		}
		return false, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	verifyResp := VerifyResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return false, err
	}
	return verifyResp.Success, nil
}

// Refresh manually refreshes the lock TTL
func (l *Lock) Refresh() error {
	refreshReq := RefreshRequest{
		Name:    l.Name,
		OwnerID: l.OwnerID,
		Token:   l.Token,
	}
	body, err := json.Marshal(refreshReq)
	if err != nil {
		return err
	}
	resp, err := http.Post(l.serverURL+"/api/refresh", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return fmt.Errorf("server error: %s", errorMsg)
		}
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	refreshResp := RefreshResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return err
	}
	if refreshResp.Lock == nil {
		return fmt.Errorf("lock refresh failed")
	}
	fmt.Printf("Lock %s refreshed\n", l.Name)
	return nil
}

// Release gracefully releases the lock
func (l *Lock) Release() error {
	// Close heartbeat channel safely (idempotent)
	select {
	case <-l.heartbeatCh:
		// Channel already closed
	default:
		close(l.heartbeatCh)
	}

	releaseReq := ReleaseRequest{
		Name:    l.Name,
		OwnerID: l.OwnerID,
		Token:   l.Token,
	}
	body, err := json.Marshal(releaseReq)
	if err != nil {
		return err
	}
	resp, err := http.Post(l.serverURL+"/api/release", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return fmt.Errorf("server error: %s", errorMsg)
		}
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	releaseResp := ReleaseResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&releaseResp); err != nil {
		return err
	}
	if !releaseResp.Success {
		return fmt.Errorf("lock release failed")
	}
	fmt.Printf("Lock %s released\n", l.Name)
	return nil
}

// IsExpired checks if the lock has likely expired based on TTL
func (l *Lock) IsExpired() bool {
	// This is a client-side estimate; server should be checked for definitive answer
	return false // For now, always return false as we don't track local timestamps
}

// TimeUntilExpiry returns the duration until the lock expires
func (l *Lock) TimeUntilExpiry() time.Duration {
	// This is approximate; use VerifyOwnership() for definitive check
	return l.TTL // For now, just return the TTL as we don't track local timestamps
}

// GetMetadata returns the lock's metadata (if any)
func (l *Lock) GetMetadata() any {
	return nil // Metadata not stored locally; would need server call
}

// Freeze freezes the lock to prevent acquisition and refresh
func (l *Lock) Freeze() error {
	resp, err := http.Post(l.serverURL+"/api/freeze/"+l.Name, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return fmt.Errorf("server error: %s", errorMsg)
		}
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	fmt.Printf("Lock %s frozen\n", l.Name)
	return nil
}

// Unfreeze unfreezes the lock to allow acquisition and refresh
func (l *Lock) Unfreeze() error {
	resp, err := http.Post(l.serverURL+"/api/unfreeze/"+l.Name, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		if errorMsg, exists := errorResp["error"]; exists {
			return fmt.Errorf("server error: %s", errorMsg)
		}
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	fmt.Printf("Lock %s unfrozen\n", l.Name)
	return nil
}

// UpdateMetadata updates the lock's metadata on the server
func (l *Lock) UpdateMetadata(metadata any) error {
	// This would require an update call to the server
	return fmt.Errorf("metadata update not implemented yet")
}

// String returns a string representation of the lock
func (l *Lock) String() string {
	return fmt.Sprintf("Lock{Name: %s, Owner: %s, Token: %d, TTL: %v}",
		l.Name, l.Owner, l.Token, l.TTL)
}
