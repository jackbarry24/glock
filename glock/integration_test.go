package glock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"glock-server/core"

	"github.com/gin-gonic/gin"
)

// TestServer represents a test glock server instance
type TestServer struct {
	server    *http.Server
	core      *core.GlockServer
	url       string
	closeFunc func()
}

// NewTestServer creates a new test server on a random available port
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	gin.SetMode(gin.TestMode)

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create server core
	config := core.DefaultConfig()
	config.Capacity = 100 // Higher capacity for tests
	coreServer := &core.GlockServer{
		Capacity: config.Capacity,
		Locks:    sync.Map{},
		Config:   config,
	}

	// Setup Gin router
	r := gin.New()
	r.Use(gin.Recovery())

	// API routes group
	api := r.Group("/api")
	{
		api.POST("/create", func(c *gin.Context) { core.CreateHandler(c, coreServer) })
		api.POST("/update", func(c *gin.Context) { core.UpdateHandler(c, coreServer) })
		api.DELETE("/delete/:name", func(c *gin.Context) { core.DeleteHandler(c, coreServer) })
		api.POST("/acquire", func(c *gin.Context) { core.AcquireHandler(c, coreServer) })
		api.POST("/refresh", func(c *gin.Context) { core.RefreshHandler(c, coreServer) })
		api.POST("/verify", func(c *gin.Context) { core.VerifyHandler(c, coreServer) })
		api.POST("/release", func(c *gin.Context) { core.ReleaseHandler(c, coreServer) })
		api.POST("/poll", func(c *gin.Context) { core.PollHandler(c, coreServer) })
		api.POST("/remove", func(c *gin.Context) { core.RemoveFromQueueHandler(c, coreServer) })
		api.GET("/status", func(c *gin.Context) { core.StatusHandler(c, coreServer) })
		api.GET("/list", func(c *gin.Context) { core.ListHandler(c, coreServer) })
		api.GET("/metrics/:name", func(c *gin.Context) { core.MetricsHandler(c, coreServer) })
	}

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: r,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()

	// Wait for server to be ready
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for i := 0; i < 50; i++ {
		resp, err := http.Get(url + "/api/status")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
		if i == 49 {
			t.Fatalf("server failed to start")
		}
	}

	closeFunc := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("server shutdown error: %v", err)
		}
	}

	return &TestServer{
		server:    server,
		core:      coreServer,
		url:       url,
		closeFunc: closeFunc,
	}
}

// Close shuts down the test server
func (ts *TestServer) Close() {
	if ts.closeFunc != nil {
		ts.closeFunc()
	}
}

// URL returns the test server URL
func (ts *TestServer) URL() string {
	return ts.url
}

// Core returns the underlying server core for direct testing if needed
func (ts *TestServer) Core() *core.GlockServer {
	return ts.core
}

// TestIntegrationBasicLifecycle tests the complete lock lifecycle end-to-end
func TestIntegrationBasicLifecycle(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Create client
	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Test create lock
	err = client.CreateLock("test-lock", "30s", "5m", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Verify lock exists on server
	_, exists := server.Core().Locks.Load("test-lock")
	if !exists {
		t.Fatal("lock was not created on server")
	}

	// Test acquire lock
	lock, err := client.Acquire("test-lock")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if lock == nil {
		t.Fatal("acquired lock is nil")
	}

	// Verify server state after acquisition
	serverLockVal, _ := server.Core().Locks.Load("test-lock")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "test-owner" {
		t.Fatalf("server owner mismatch: expected 'test-owner', got '%s'", serverLock.Owner)
	}
	if serverLock.OwnerID != client.ID {
		t.Fatalf("server ownerID mismatch: expected '%s', got '%s'", client.ID, serverLock.OwnerID)
	}
	if serverLock.Available {
		t.Fatal("lock should not be available after acquisition")
	}

	// Test that client and server have consistent state
	if lock.Name != serverLock.Name {
		t.Fatalf("client/server name mismatch: client=%s, server=%s", lock.Name, serverLock.Name)
	}
	if lock.Owner != serverLock.Owner {
		t.Fatalf("client/server owner mismatch: client=%s, server=%s", lock.Owner, serverLock.Owner)
	}
	if lock.Token != serverLock.Token {
		t.Fatalf("client/server token mismatch: client=%d, server=%d", lock.Token, serverLock.Token)
	}

	// Test release
	err = lock.Release()
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// Verify server state after release
	serverLockVal, _ = server.Core().Locks.Load("test-lock")
	serverLock = serverLockVal.(*core.Lock)
	if !serverLock.Available {
		t.Fatal("lock should be available after release")
	}
	if serverLock.Owner != "" {
		t.Fatalf("lock owner should be empty after release, got '%s'", serverLock.Owner)
	}
}

// TestIntegrationHeartbeatMechanism tests heartbeat functionality end-to-end
func TestIntegrationHeartbeatMechanism(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Create and acquire lock with short TTL
	err = client.CreateLock("heartbeat-test", "100ms", "1s", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	lock, err := client.Acquire("heartbeat-test")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Start heartbeat
	lock.StartHeartbeat()

	// Wait for initial heartbeat interval
	time.Sleep(50 * time.Millisecond)

	// Verify server still shows lock as held by client
	serverLockVal, _ := server.Core().Locks.Load("heartbeat-test")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "test-owner" {
		t.Fatalf("lock should still be held by client after heartbeat")
	}
	if serverLock.Available {
		t.Fatal("lock should not be available")
	}

	// Wait longer than original TTL but within heartbeat interval
	time.Sleep(100 * time.Millisecond)

	// Verify server still shows lock as held (heartbeat should have refreshed)
	serverLockVal, _ = server.Core().Locks.Load("heartbeat-test")
	serverLock = serverLockVal.(*core.Lock)
	if serverLock.Owner != "test-owner" {
		t.Fatalf("lock should still be held by client after heartbeat refresh")
	}
	if serverLock.Available {
		t.Fatal("lock should not be available after refresh")
	}

	// Stop heartbeat and wait for expiration
	lock.Release()
	time.Sleep(150 * time.Millisecond)

	// Verify lock is now available
	serverLockVal, _ = server.Core().Locks.Load("heartbeat-test")
	serverLock = serverLockVal.(*core.Lock)
	if !serverLock.Available {
		t.Fatal("lock should be available after TTL expiration")
	}
}

// TestIntegrationQueueFunctionality tests queue operations end-to-end
func TestIntegrationQueueFunctionality(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Create clients
	client1, err := Connect(server.URL(), "owner1")
	if err != nil {
		t.Fatalf("failed to connect client1: %v", err)
	}
	client2, err := Connect(server.URL(), "owner2")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Create lock with FIFO queue
	err = client1.CreateLock("queue-test", "1s", "5m", QueueFIFO, "10s")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Client1 acquires the lock
	lock1, err := client1.Acquire("queue-test")
	if err != nil {
		t.Fatalf("failed to acquire lock for client1: %v", err)
	}

	// Client2 tries to acquire - should get queued
	lock2, queueResp, err := client2.AcquireOrQueue("queue-test")
	if err != nil {
		t.Fatalf("failed to queue acquisition for client2: %v", err)
	}
	if lock2 != nil {
		t.Fatal("client2 should not have acquired lock immediately")
	}
	if queueResp == nil {
		t.Fatal("client2 should have received queue response")
	}
	if queueResp.Position != 1 {
		t.Fatalf("client2 should be position 1 in queue, got %d", queueResp.Position)
	}

	// Verify server state
	serverLockVal, _ := server.Core().Locks.Load("queue-test")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "owner1" {
		t.Fatalf("server should show lock held by owner1, got %s", serverLock.Owner)
	}

	// Poll queue - should still be waiting
	pollResp, err := client2.PollQueue("queue-test", queueResp.RequestID)
	if err != nil {
		t.Fatalf("failed to poll queue: %v", err)
	}
	if pollResp.Status != "waiting" {
		t.Fatalf("expected status 'waiting', got '%s'", pollResp.Status)
	}

	// Release lock from client1
	err = lock1.Release()
	if err != nil {
		t.Fatalf("failed to release lock from client1: %v", err)
	}

	// Poll again - should now be ready
	time.Sleep(50 * time.Millisecond) // Allow time for queue processing
	pollResp, err = client2.PollQueue("queue-test", queueResp.RequestID)
	if err != nil {
		t.Fatalf("failed to poll queue after release: %v", err)
	}
	if pollResp.Status != "ready" {
		t.Fatalf("expected status 'ready', got '%s'", pollResp.Status)
	}
	if pollResp.Lock == nil {
		t.Fatal("should have received lock in poll response")
	}

	// Verify server state after queue processing
	serverLockVal, _ = server.Core().Locks.Load("queue-test")
	serverLock = serverLockVal.(*core.Lock)
	if serverLock.Owner != "owner2" {
		t.Fatalf("server should show lock held by owner2, got %s", serverLock.Owner)
	}
	if serverLock.Available {
		t.Fatal("lock should not be available")
	}
}

// TestIntegrationErrorHandling tests error conditions and edge cases
func TestIntegrationErrorHandling(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "owner1")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Test acquiring non-existent lock
	_, err = client.Acquire("nonexistent")
	if err == nil {
		t.Fatal("should have failed to acquire non-existent lock")
	}

	// Test double acquisition (should fail)
	err = client.CreateLock("double-acquire-test", "1s", "5m", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	lock1, err := client.Acquire("double-acquire-test")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Create second client and try to acquire same lock
	client2, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	lock2, err := client2.Acquire("double-acquire-test")
	if err == nil {
		t.Fatal("should have failed to acquire already held lock")
	}
	if lock2 != nil {
		t.Fatal("should not have received lock for already held resource")
	}

	// Verify server still shows original owner
	serverLockVal, _ := server.Core().Locks.Load("double-acquire-test")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "owner1" {
		t.Fatalf("server should still show original owner, got %s", serverLock.Owner)
	}

	// Test invalid token operations
	err = lock1.Release()
	if err != nil {
		t.Fatalf("original owner should be able to release: %v", err)
	}

	// Try to release again with stale token (should fail gracefully)
	err = lock1.Release()
	if err == nil {
		t.Fatal("should have failed to release with stale token")
	}

	// Verify server state after release
	serverLockVal, _ = server.Core().Locks.Load("double-acquire-test")
	serverLock = serverLockVal.(*core.Lock)
	if !serverLock.Available {
		t.Fatal("lock should be available after release")
	}
}

// TestIntegrationConcurrentAccess tests concurrent client access patterns
func TestIntegrationConcurrentAccess(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Create lock directly on server
	_, _, err := server.Core().CreateLock(&core.CreateRequest{
		Name:   "concurrent-test",
		TTL:    "30s",
		MaxTTL: "5m",
	})
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Launch multiple clients trying to acquire concurrently
	const numClients = 5
	results := make(chan *acquireResult, numClients)
	var locksToRelease []*Lock

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			client, _ := Connect(server.URL(), fmt.Sprintf("client-%d", clientID))
			lock, err := client.Acquire("concurrent-test")
			results <- &acquireResult{lock: lock, err: err, clientID: clientID}
		}(i)
	}

	// Collect results
	var successfulAcquires int
	var failedAcquires int

	for i := 0; i < numClients; i++ {
		result := <-results
		if result.err == nil {
			successfulAcquires++
			locksToRelease = append(locksToRelease, result.lock)

			// Verify the successful client owns the lock
			serverLockVal, _ := server.Core().Locks.Load("concurrent-test")
			serverLock := serverLockVal.(*core.Lock)
			expectedOwner := fmt.Sprintf("client-%d", result.clientID)
			if serverLock.Owner != expectedOwner {
				t.Errorf("server shows wrong owner: expected %s, got %s", expectedOwner, serverLock.Owner)
			}
		} else {
			failedAcquires++
		}
	}

	// Clean up: release all locks that were acquired
	for _, lock := range locksToRelease {
		if lock != nil {
			err := lock.Release()
			if err != nil {
				t.Logf("Warning: failed to release lock: %v", err)
			}
		}
	}

	// Verify exactly one client succeeded
	if successfulAcquires != 1 {
		t.Fatalf("expected exactly 1 successful acquire, got %d", successfulAcquires)
	}
	if failedAcquires != numClients-1 {
		t.Fatalf("expected %d failed acquires, got %d", numClients-1, failedAcquires)
	}

	// Additional verification: ensure server shows lock as available after releases
	time.Sleep(10 * time.Millisecond) // Allow time for release to propagate
	serverLockVal, _ := server.Core().Locks.Load("concurrent-test")
	serverLock := serverLockVal.(*core.Lock)
	if !serverLock.Available {
		t.Errorf("lock should be available after all releases, but owner is %s", serverLock.Owner)
	}
}

// TestServerSideRaceCondition tests for race conditions in the server AcquireLock method
func TestServerSideRaceCondition(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Create lock directly
	_, _, err := server.Core().CreateLock(&core.CreateRequest{
		Name:   "race-test",
		TTL:    "30s",
		MaxTTL: "5m",
	})
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Test concurrent calls to AcquireLock method directly (bypassing HTTP)
	const numConcurrent = 10
	var wg sync.WaitGroup
	results := make(chan *testAcquireResult, numConcurrent)

	wg.Add(numConcurrent)
	for i := 0; i < numConcurrent; i++ {
		go func(clientID int) {
			defer wg.Done()

			req := &core.AcquireRequest{
				Name:         "race-test",
				Owner:        fmt.Sprintf("client-%d", clientID),
				OwnerID:      fmt.Sprintf("uuid-%d", clientID),
				QueueRequest: &[]bool{false}[0], // Don't queue
			}

			// Call AcquireLock directly
			result, code, err := server.Core().AcquireLock(req)
			results <- &testAcquireResult{
				result:   result,
				code:     code,
				err:      err,
				clientID: clientID,
			}
		}(i)
	}

	wg.Wait()
	close(results)

	// Analyze results
	successCount := 0
	failureCount := 0
	queuedCount := 0
	var successfulClients []int
	var failedClients []int

	for res := range results {
		t.Logf("Client %d: code=%d, err=%v", res.clientID, res.code, res.err)
		if res.err == nil && res.code == 200 {
			successCount++
			successfulClients = append(successfulClients, res.clientID)
		} else if res.code == 202 { // Queued
			queuedCount++
		} else {
			failureCount++
			failedClients = append(failedClients, res.clientID)
		}
	}

	t.Logf("Summary: %d successful, %d queued, %d failed", successCount, queuedCount, failureCount)

	// Exactly one should succeed
	if successCount != 1 {
		t.Errorf("Server race condition: expected 1 successful acquisition, got %d", successCount)
		for _, clientID := range successfulClients {
			t.Logf("Successful client: %d", clientID)
		}
	}

	// All requests should have some result
	totalResults := successCount + queuedCount + failureCount
	if totalResults != numConcurrent {
		t.Errorf("Expected %d total results, got %d (success:%d + queued:%d + failed:%d)",
			numConcurrent, totalResults, successCount, queuedCount, failureCount)
	}
}

type testAcquireResult struct {
	result   interface{}
	code     int
	err      error
	clientID int
}

type acquireResult struct {
	lock     *Lock
	err      error
	clientID int
}

// TestHTTPClientRaceCondition tests for race conditions at the HTTP client level
func TestHTTPClientRaceCondition(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Create lock
	_, _, err := server.Core().CreateLock(&core.CreateRequest{
		Name:   "http-race-test",
		TTL:    "30s",
		MaxTTL: "5m",
	})
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	const numConcurrent = 5
	var wg sync.WaitGroup
	results := make(chan *httpTestResult, numConcurrent)

	wg.Add(numConcurrent)
	for i := 0; i < numConcurrent; i++ {
		go func(clientID int) {
			defer wg.Done()
			client, _ := Connect(server.URL(), fmt.Sprintf("client-%d", clientID))

			// Manually make the HTTP request to capture response details
			queueRequest := false
			acquireReq := AcquireRequest{
				Name:         "http-race-test",
				Owner:        client.Owner,
				OwnerID:      client.ID,
				QueueRequest: &queueRequest,
			}
			body, _ := json.Marshal(acquireReq)

			resp, httpErr := client.getHTTPClient().Post(client.ServerURL+"/api/acquire", "application/json", bytes.NewBuffer(body))
			if httpErr != nil {
				results <- &httpTestResult{clientID: clientID, httpErr: httpErr}
				return
			}
			defer resp.Body.Close()

			// Read the raw response body
			respBody, _ := io.ReadAll(resp.Body)
			t.Logf("Client %d: HTTP %d, Body: %s", clientID, resp.StatusCode, string(respBody))

			// Try to parse as normal response
			var acquireResp AcquireResponse
			if jsonErr := json.Unmarshal(respBody, &acquireResp); jsonErr != nil {
				results <- &httpTestResult{clientID: clientID, jsonErr: jsonErr, statusCode: resp.StatusCode, rawBody: string(respBody)}
				return
			}

			results <- &httpTestResult{
				clientID:   clientID,
				statusCode: resp.StatusCode,
				lock:       acquireResp.Lock,
				rawBody:    string(respBody),
			}
		}(i)
	}

	wg.Wait()
	close(results)

	// Analyze results
	successCount := 0
	var successfulClients []int

	for res := range results {
		t.Logf("Client %d result: status=%d, lock=%v, httpErr=%v, jsonErr=%v",
			res.clientID, res.statusCode, res.lock != nil, res.httpErr, res.jsonErr)

		if res.httpErr == nil && res.jsonErr == nil && res.statusCode == 200 && res.lock != nil {
			successCount++
			successfulClients = append(successfulClients, res.clientID)
		}
	}

	t.Logf("Total successful acquisitions: %d", successCount)
	for _, clientID := range successfulClients {
		t.Logf("Successful client: %d", clientID)
	}

	if successCount != 1 {
		t.Errorf("HTTP race condition: expected 1 successful acquisition, got %d", successCount)
	}
}

type httpTestResult struct {
	clientID   int
	statusCode int
	lock       *Lock
	httpErr    error
	jsonErr    error
	rawBody    string
}

// TestIntegrationNewFeatures tests all newly added client features end-to-end
func TestIntegrationNewFeatures(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Test CreateLockWithMetadata
	metadata := map[string]interface{}{
		"description": "test lock",
		"priority":    1,
	}
	err = client.CreateLockWithMetadata("metadata-test", "30s", "5m", QueueNone, "1m", metadata)
	if err != nil {
		t.Fatalf("failed to create lock with metadata: %v", err)
	}

	// Test UpdateLock
	err = client.UpdateLock("metadata-test", "60s", "10m", QueueFIFO, "2m", map[string]interface{}{"updated": true})
	if err != nil {
		t.Fatalf("failed to update lock: %v", err)
	}

	// Test Acquire and VerifyOwnership
	lock, err := client.Acquire("metadata-test")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Test VerifyOwnership
	owns, err := lock.VerifyOwnership()
	if err != nil {
		t.Fatalf("failed to verify ownership: %v", err)
	}
	if !owns {
		t.Fatal("client should own the lock")
	}

	// Test manual Refresh
	err = lock.Refresh()
	if err != nil {
		t.Fatalf("failed to refresh lock: %v", err)
	}

	// Test GetOwnedLocks
	ownedLocks := client.GetOwnedLocks()
	if len(ownedLocks) != 1 {
		t.Fatalf("expected 1 owned lock, got %d", len(ownedLocks))
	}

	// Test GetLockByName
	retrievedLock, exists := client.GetLockByName("metadata-test")
	if !exists {
		t.Fatal("lock should exist in client cache")
	}
	if retrievedLock.Name != "metadata-test" {
		t.Fatal("retrieved lock name mismatch")
	}

	// Test ListLocks
	lockNames, err := client.ListLocks()
	if err != nil {
		t.Fatalf("failed to list locks: %v", err)
	}
	if len(lockNames) == 0 {
		t.Fatal("should have at least one lock")
	}

	// Test GetServerStatus
	status, err := client.GetServerStatus()
	if err != nil {
		t.Fatalf("failed to get server status: %v", err)
	}
	if status == nil {
		t.Fatal("server status should not be nil")
	}

	// Test HealthCheck
	err = client.HealthCheck()
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	// Test lock utility methods
	if lock.IsExpired() {
		t.Fatal("lock should not be expired")
	}

	timeUntilExpiry := lock.TimeUntilExpiry()
	if timeUntilExpiry <= 0 {
		t.Fatal("time until expiry should be positive")
	}

	lockStr := lock.String()
	if lockStr == "" {
		t.Fatal("lock string representation should not be empty")
	}

	// Test ReleaseAllLocks
	err = lock.Release()
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	errors := client.ReleaseAllLocks()
	if len(errors) > 0 {
		t.Fatalf("release all locks should not return errors: %v", errors)
	}

	// Test DeleteLock
	err = client.DeleteLock("metadata-test")
	if err != nil {
		t.Fatalf("failed to delete lock: %v", err)
	}
}

// TestIntegrationClientConfiguration tests client configuration with real server
func TestIntegrationClientConfiguration(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	// Test default configuration
	config := DefaultClientConfig(server.URL())
	if config.ServerURL != server.URL() {
		t.Fatal("default config server URL mismatch")
	}

	// Test ConnectWithConfig
	client, err := ConnectWithConfig(config, "test-owner")
	if err != nil {
		t.Fatalf("failed to connect with config: %v", err)
	}

	// Verify client was created with correct URL
	if client.ServerURL != server.URL() {
		t.Fatal("client server URL mismatch")
	}

	// Test basic functionality with configured client
	err = client.CreateLock("config-test", "30s", "5m", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock with configured client: %v", err)
	}
}

// TestIntegrationAcquireOrWaitSuccess tests successful AcquireOrWait with immediate acquisition
func TestIntegrationAcquireOrWaitSuccess(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Create lock with no queue
	err = client.CreateLock("immediate-acquire-test", "1s", "5m", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// AcquireOrWait should succeed immediately
	lock, err := client.AcquireOrWait("immediate-acquire-test", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireOrWait should succeed immediately: %v", err)
	}
	if lock == nil {
		t.Fatal("should have received lock immediately")
	}
	if lock.Name != "immediate-acquire-test" {
		t.Fatal("lock name mismatch")
	}

	// Verify server state
	serverLockVal, _ := server.Core().Locks.Load("immediate-acquire-test")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "test-owner" {
		t.Fatalf("server should show lock held by test-owner, got %s", serverLock.Owner)
	}
}

// TestIntegrationAcquireOrWaitQueueSuccess tests AcquireOrWait with queue that eventually succeeds
func TestIntegrationAcquireOrWaitQueueSuccess(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client1, err := Connect(server.URL(), "owner1")
	if err != nil {
		t.Fatalf("failed to connect client1: %v", err)
	}

	client2, err := Connect(server.URL(), "owner2")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Create lock with FIFO queue
	err = client1.CreateLock("queue-wait-test", "200ms", "5m", QueueFIFO, "5s")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Client1 acquires the lock
	lock1, err := client1.Acquire("queue-wait-test")
	if err != nil {
		t.Fatalf("client1 failed to acquire lock: %v", err)
	}

	// Client2 tries AcquireOrWait - should wait and eventually succeed
	var lock2 *Lock
	done := make(chan bool, 1)

	go func() {
		var err error
		lock2, err = client2.AcquireOrWait("queue-wait-test", 3*time.Second)
		if err != nil {
			t.Errorf("AcquireOrWait failed: %v", err)
		}
		done <- true
	}()

	// Wait a bit then release client1's lock
	time.Sleep(100 * time.Millisecond)
	err = lock1.Release()
	if err != nil {
		t.Fatalf("failed to release lock1: %v", err)
	}

	// Wait for client2 to acquire the lock
	select {
	case <-done:
		// Success!
	case <-time.After(2 * time.Second):
		t.Fatal("AcquireOrWait timed out waiting for lock")
	}

	if lock2 == nil {
		t.Fatal("client2 should have acquired the lock")
	}
	if lock2.Name != "queue-wait-test" {
		t.Fatal("lock name mismatch")
	}

	// Verify server state
	serverLockVal, _ := server.Core().Locks.Load("queue-wait-test")
	serverLock := serverLockVal.(*core.Lock)
	if serverLock.Owner != "owner2" {
		t.Fatalf("server should show lock held by owner2, got %s", serverLock.Owner)
	}

	// Clean up: release lock2 to ensure no hanging connections
	if lock2 != nil {
		err = lock2.Release()
		if err != nil {
			t.Errorf("failed to release lock2: %v", err)
		}
	}

	// Give a moment for any background polling to complete
	time.Sleep(100 * time.Millisecond)
}

// TestIntegrationAcquireOrWaitTimeout tests AcquireOrWait timeout behavior
func TestIntegrationAcquireOrWaitTimeout(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client1, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client1: %v", err)
	}

	client2, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Create lock with FIFO queue
	err = client1.CreateLock("timeout-test", "1s", "5m", QueueFIFO, "5s")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Client1 acquires the lock and holds it
	_, err = client1.Acquire("timeout-test")
	if err != nil {
		t.Fatalf("client1 failed to acquire lock: %v", err)
	}

	// Client2 tries AcquireOrWait with short timeout - should timeout
	start := time.Now()
	lock2, err := client2.AcquireOrWait("timeout-test", 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("AcquireOrWait should have timed out")
	}
	if lock2 != nil {
		t.Fatal("should not have received lock after timeout")
	}
	if elapsed < 150*time.Millisecond || elapsed > 300*time.Millisecond {
		t.Fatalf("timeout took %v, expected around 200ms", elapsed)
	}

	// Verify error message contains timeout info
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error should mention timeout, got: %v", err)
	}

	// Verify the queue entry was cleaned up
	// We can't easily verify this directly, but the test passing indicates
	// the RemoveFromQueue call succeeded (no panic/error in AcquireOrWait)
}

// TestIntegrationRemoveFromQueue tests the RemoveFromQueue functionality
func TestIntegrationRemoveFromQueue(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client1, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client1: %v", err)
	}

	client2, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Create lock with FIFO queue
	err = client1.CreateLock("remove-queue-test", "1s", "5m", QueueFIFO, "5s")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Client1 acquires the lock
	lock1, err := client1.Acquire("remove-queue-test")
	if err != nil {
		t.Fatalf("client1 failed to acquire lock: %v", err)
	}

	// Client2 tries to acquire - should get queued
	lock2, queueResp, err := client2.AcquireOrQueue("remove-queue-test")
	if err != nil {
		t.Fatalf("client2 failed to queue: %v", err)
	}
	if lock2 != nil || queueResp == nil {
		t.Fatal("client2 should have been queued")
	}

	// Manually remove the queue entry
	err = client2.RemoveFromQueue("remove-queue-test", queueResp.RequestID)
	// Note: RemoveFromQueue can fail with 404 if the request has already expired
	// or been processed, which is acceptable behavior
	if err != nil && !strings.Contains(err.Error(), "404") {
		t.Fatalf("unexpected error removing from queue: %v", err)
	}

	// Try to poll the removed request - should get not_found
	pollResp, err := client2.PollQueue("remove-queue-test", queueResp.RequestID)
	if err != nil {
		t.Fatalf("poll after removal failed: %v", err)
	}
	if pollResp.Status != "not_found" {
		t.Fatalf("expected not_found after removal, got '%s'", pollResp.Status)
	}

	// Release client1's lock
	err = lock1.Release()
	if err != nil {
		t.Fatalf("failed to release lock1: %v", err)
	}

	// Try to acquire with a new client - should succeed immediately
	client3, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client3: %v", err)
	}

	lock3, err := client3.Acquire("remove-queue-test")
	if err != nil {
		t.Fatalf("client3 should be able to acquire immediately: %v", err)
	}
	if lock3 == nil {
		t.Fatal("client3 should have received the lock")
	}
}

// TestIntegrationAcquireOrWaitZeroTimeout tests AcquireOrWait with zero timeout
func TestIntegrationAcquireOrWaitZeroTimeout(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Create lock and acquire it
	err = client.CreateLock("zero-timeout-test", "1s", "5m", QueueNone, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	lock, err := client.Acquire("zero-timeout-test")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Create second client
	client2, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Try AcquireOrWait with zero timeout - should fail immediately
	start := time.Now()
	lock2, err := client2.AcquireOrWait("zero-timeout-test", 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("AcquireOrWait with zero timeout should fail")
	}
	if lock2 != nil {
		t.Fatal("should not receive lock with zero timeout")
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("zero timeout should fail immediately, took %v", elapsed)
	}

	// Release first lock
	err = lock.Release()
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}
}

// TestIntegrationAcquireOrWaitExpiredRequest tests AcquireOrWait with expired queue request
func TestIntegrationAcquireOrWaitExpiredRequest(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client1, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client1: %v", err)
	}

	client2, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client2: %v", err)
	}

	// Create lock with very short queue timeout
	err = client1.CreateLock("expired-queue-test", "1s", "5m", QueueFIFO, "100ms")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Client1 acquires the lock
	_, err = client1.Acquire("expired-queue-test")
	if err != nil {
		t.Fatalf("client1 failed to acquire lock: %v", err)
	}

	// Client2 tries to queue
	_, queueResp, err := client2.AcquireOrQueue("expired-queue-test")
	if err != nil {
		t.Fatalf("client2 failed to queue: %v", err)
	}
	if queueResp == nil {
		t.Fatal("client2 should have been queued")
	}

	// Wait for queue timeout to expire
	time.Sleep(150 * time.Millisecond)

	// Client2 tries AcquireOrWait - should get expired error
	lock2, err := client2.AcquireOrWait("expired-queue-test", 1*time.Second)
	if err == nil {
		t.Fatal("AcquireOrWait should fail with expired request")
	}
	if lock2 != nil {
		t.Fatal("should not receive lock with expired request")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error should mention expired, got: %v", err)
	}
}

// TestIntegrationMetricsAPI tests the metrics API endpoint
func TestIntegrationMetricsAPI(t *testing.T) {
	server := NewTestServer(t)
	defer server.Close()

	client, err := Connect(server.URL(), "test-owner")
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Create a lock
	err = client.CreateLock("metrics-test", "30s", "5m", QueueFIFO, "1m")
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}

	// Acquire the lock
	lock, err := client.Acquire("metrics-test")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Release the lock
	err = lock.Release()
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// Test metrics endpoint
	resp, err := http.Get(server.URL() + "/api/metrics/metrics-test")
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var metricsResp core.MetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&metricsResp); err != nil {
		t.Fatalf("failed to decode metrics response: %v", err)
	}

	// Verify metrics data
	if metricsResp.LockName != "metrics-test" {
		t.Fatalf("expected lock name 'metrics-test', got '%s'", metricsResp.LockName)
	}

	if metricsResp.Metrics == nil {
		t.Fatal("metrics should not be nil")
	}

	// Check that we have some basic metrics
	if metricsResp.Metrics.TotalAcquireAttempts < 1 {
		t.Fatalf("expected at least 1 acquire attempt, got %d", metricsResp.Metrics.TotalAcquireAttempts)
	}

	if metricsResp.Metrics.SuccessfulAcquires < 1 {
		t.Fatalf("expected at least 1 successful acquire, got %d", metricsResp.Metrics.SuccessfulAcquires)
	}

	// Check that created timestamp is set
	if metricsResp.Metrics.CreatedAt.IsZero() {
		t.Fatal("created_at should not be zero")
	}
}
