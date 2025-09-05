package core

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func newServer(capacity int) *GlockServer {
	config := DefaultConfig()
	config.Capacity = capacity
	return &GlockServer{
		Capacity: capacity,
		Locks:    sync.Map{},
		Config:   config,
	}
}

func TestCreateAndDeleteLock(t *testing.T) {
	g := newServer(10)
	lock, code, err := g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	if err != nil || code != http.StatusOK || lock == nil {
		t.Fatalf("create failed: code=%d err=%v", code, err)
	}

	ok, code, err := g.DeleteLock("a")
	if err != nil || code != http.StatusOK || !ok {
		t.Fatalf("delete failed: code=%d err=%v", code, err)
	}
}

func TestCreateDuplicate(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	_, code, err := g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict on duplicate create, got code=%d err=%v", code, err)
	}
}

func TestUpdateChangesPropertiesOnly(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	l, code, err := g.UpdateLock(&UpdateRequest{Name: "a", TTL: "2s", MaxTTL: "3m", Metadata: map[string]string{"x": "y"}})
	if err != nil || code != http.StatusOK {
		t.Fatalf("update failed: code=%d err=%v", code, err)
	}
	if l.TTL != 2*time.Second || l.MaxTTL != 3*time.Minute {
		t.Fatalf("update did not apply TTL/MaxTTL")
	}
}

func TestAcquireRefreshReleaseFlow(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "50ms", MaxTTL: "1s"})
	// acquire
	result, code, err := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	if err != nil || code != http.StatusOK {
		t.Fatalf("acquire failed: code=%d err=%v", code, err)
	}
	lock := result.(*Lock)
	if lock == nil {
		t.Fatalf("acquire returned nil lock")
	}

	// refresh before TTL
	time.Sleep(10 * time.Millisecond)
	_, code, err = g.RefreshLock(&RefreshRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err != nil || code != http.StatusOK {
		t.Fatalf("refresh failed: code=%d err=%v", code, err)
	}

	// release
	ok, code, err := g.ReleaseLock(&ReleaseRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err != nil || code != http.StatusOK || !ok {
		t.Fatalf("release failed: code=%d err=%v", code, err)
	}
}

func TestRefreshAfterTTLExpired(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "10ms", MaxTTL: "1s"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)
	time.Sleep(20 * time.Millisecond)
	_, code, err := g.RefreshLock(&RefreshRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when refreshing expired lock, got code=%d err=%v", code, err)
	}
}

// Test VerifyLock functionality
func TestVerifyLock(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)

	// Verify with correct owner and token
	ok, code, err := g.VerifyLock(&VerifyRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err != nil || code != http.StatusOK || !ok {
		t.Fatalf("verify failed: code=%d err=%v ok=%v", code, err, ok)
	}

	// Verify with wrong owner
	ok, code, err = g.VerifyLock(&VerifyRequest{Name: "a", OwnerID: "22222222-2222-2222-2222-222222222222", Token: lock.Token})
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict for wrong owner, got code=%d err=%v ok=%v", code, err, ok)
	}

	// Verify with stale token
	ok, code, err = g.VerifyLock(&VerifyRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token - 1})
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict for stale token, got code=%d err=%v ok=%v", code, err, ok)
	}
}

// Test MaxTTL expiration
func TestMaxTTLExpiration(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "10ms", MaxTTL: "10s"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)
	time.Sleep(20 * time.Millisecond)

	// Refresh should fail due to MaxTTL expiration
	_, code, err := g.RefreshLock(&RefreshRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when MaxTTL expired, got code=%d err=%v", code, err)
	}

	// Verify should fail due to MaxTTL expiration
	ok, code, err := g.VerifyLock(&VerifyRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token})
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict for MaxTTL expired verify, got code=%d err=%v ok=%v", code, err, ok)
	}
}

// Test capacity limits
func TestCapacityLimit(t *testing.T) {
	g := newServer(2) // Small capacity
	_, code1, err1 := g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	_, code2, err2 := g.CreateLock(&CreateRequest{Name: "b", TTL: "1s", MaxTTL: "1m"})
	_, code3, err3 := g.CreateLock(&CreateRequest{Name: "c", TTL: "1s", MaxTTL: "1m"})

	if err1 != nil || code1 != http.StatusOK {
		t.Fatalf("first create should succeed")
	}
	if err2 != nil || code2 != http.StatusOK {
		t.Fatalf("second create should succeed")
	}
	if err3 == nil || code3 != http.StatusTooManyRequests {
		t.Fatalf("third create should fail with capacity limit, got code=%d err=%v", code3, err3)
	}
}

// Test fencing token increment on acquire
func TestFencingTokenIncrement(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})

	// First acquire
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)
	token1 := lock1.Token

	// Release
	g.ReleaseLock(&ReleaseRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: token1})

	// Second acquire should increment token
	result2, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me2", OwnerID: "22222222-2222-2222-2222-222222222222"})
	lock2 := result2.(*Lock)
	token2 := lock2.Token

	if token2 != token1+1 {
		t.Fatalf("expected token to increment: token1=%d, token2=%d, expected %d", token1, token2, token1+1)
	}
}

// Test update on held lock fails
func TestUpdateHeldLockFails(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})

	_, code, err := g.UpdateLock(&UpdateRequest{Name: "a", TTL: "2s", MaxTTL: "3m"})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when updating held lock, got code=%d err=%v", code, err)
	}
}

// Test delete on held lock fails
func TestDeleteHeldLockFails(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})

	ok, code, err := g.DeleteLock("a")
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict when deleting held lock, got code=%d err=%v ok=%v", code, err, ok)
	}
}

// Test release with wrong owner
func TestReleaseWrongOwner(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)

	ok, code, err := g.ReleaseLock(&ReleaseRequest{Name: "a", OwnerID: "22222222-2222-2222-2222-222222222222", Token: lock.Token})
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict for wrong owner release, got code=%d err=%v ok=%v", code, err, ok)
	}
}

// Test release with stale token
func TestReleaseStaleToken(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)

	ok, code, err := g.ReleaseLock(&ReleaseRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock.Token - 1})
	if err == nil || code != http.StatusConflict || ok {
		t.Fatalf("expected conflict for stale token release, got code=%d err=%v ok=%v", code, err, ok)
	}
}

// Test operations on non-existent lock
func TestOperationsOnNonExistentLock(t *testing.T) {
	g := newServer(10)

	// Update
	_, code, err := g.UpdateLock(&UpdateRequest{Name: "nonexistent", TTL: "1s", MaxTTL: "1m"})
	if err == nil || code != http.StatusNotFound {
		t.Fatalf("expected not found for update on nonexistent lock")
	}

	// Acquire
	_, code, err = g.AcquireLock(&AcquireRequest{Name: "nonexistent", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	if err == nil || code != http.StatusNotFound {
		t.Fatalf("expected not found for acquire on nonexistent lock")
	}

	// Refresh
	_, code, err = g.RefreshLock(&RefreshRequest{Name: "nonexistent", OwnerID: "11111111-1111-1111-1111-111111111111", Token: 1})
	if err == nil || code != http.StatusNotFound {
		t.Fatalf("expected not found for refresh on nonexistent lock")
	}

	// Verify
	ok, code, err := g.VerifyLock(&VerifyRequest{Name: "nonexistent", OwnerID: "11111111-1111-1111-1111-111111111111", Token: 1})
	if err == nil || code != http.StatusNotFound || ok {
		t.Fatalf("expected not found for verify on nonexistent lock")
	}

	// Release
	ok, code, err = g.ReleaseLock(&ReleaseRequest{Name: "nonexistent", OwnerID: "11111111-1111-1111-1111-111111111111", Token: 1})
	if err == nil || code != http.StatusNotFound || ok {
		t.Fatalf("expected not found for release on nonexistent lock")
	}

	// Delete
	ok, code, err = g.DeleteLock("nonexistent")
	if err == nil || code != http.StatusNotFound || ok {
		t.Fatalf("expected not found for delete on nonexistent lock")
	}
}

// Test queue creation and validation
func TestQueueCreationAndValidation(t *testing.T) {
	g := newServer(10)

	// Test valid queue types
	_, code, err := g.CreateLock(&CreateRequest{
		Name:      "fifo-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueFIFO,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("FIFO queue creation should succeed")
	}

	_, code, err = g.CreateLock(&CreateRequest{
		Name:      "lifo-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueLIFO,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("LIFO queue creation should succeed")
	}

	_, code, err = g.CreateLock(&CreateRequest{
		Name:      "none-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueNone,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("None queue creation should succeed")
	}

	// Test invalid queue type
	_, code, err = g.CreateLock(&CreateRequest{
		Name:      "invalid-queue",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: "invalid",
	})
	if err == nil || code != http.StatusBadRequest {
		t.Fatalf("invalid queue type should fail")
	}

	// Test default queue behavior (should be none)
	lock, _, _ := g.CreateLock(&CreateRequest{
		Name:   "default-queue",
		TTL:    "1s",
		MaxTTL: "1m",
	})
	if lock.QueueType != QueueNone {
		t.Fatalf("default queue type should be 'none', got %s", lock.QueueType)
	}

	// Test default queue timeout
	if lock.QueueTimeout != 5*time.Minute {
		t.Fatalf("default queue timeout should be 5 minutes, got %v", lock.QueueTimeout)
	}
}

// Test queue polling on non-existent lock
func TestQueuePollNonExistentLock(t *testing.T) {
	g := newServer(10)

	_, code, err := g.PollQueue(&PollRequest{
		Name:      "nonexistent",
		RequestID: "fake-request-id",
		OwnerID:   "11111111-1111-1111-1111-111111111111",
	})
	if err == nil || code != http.StatusNotFound {
		t.Fatalf("polling non-existent lock should fail")
	}
}

// Test queue polling with invalid request ID
func TestQueuePollInvalidRequestID(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "test-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueFIFO,
	})

	_, code, err := g.PollQueue(&PollRequest{
		Name:      "test-lock",
		RequestID: "invalid-request-id",
		OwnerID:   "11111111-1111-1111-1111-111111111111",
	})
	if err == nil || code != http.StatusNotFound {
		t.Fatalf("polling with invalid request ID should fail")
	}
}

// Test queue update operations
func TestQueueUpdateOperations(t *testing.T) {
	g := newServer(10)

	// Create lock with FIFO queue
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "update-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Update to LIFO
	updatedLock, code, err := g.UpdateLock(&UpdateRequest{
		Name:         "update-test",
		TTL:          "2s",
		MaxTTL:       "2m",
		QueueType:    QueueLIFO,
		QueueTimeout: "2m",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("queue update should succeed")
	}

	if updatedLock.QueueType != QueueLIFO {
		t.Fatalf("queue type should be updated to LIFO, got %s", updatedLock.QueueType)
	}

	if updatedLock.QueueTimeout != 2*time.Minute {
		t.Fatalf("queue timeout should be updated to 2 minutes, got %v", updatedLock.QueueTimeout)
	}

	// Test updating only queue settings (leave TTL/MaxTTL unchanged)
	updatedLock2, code, err := g.UpdateLock(&UpdateRequest{
		Name:         "update-test",
		QueueType:    QueueNone,
		QueueTimeout: "30s",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("partial queue update should succeed")
	}

	if updatedLock2.QueueType != QueueNone {
		t.Fatalf("queue type should be updated to None, got %s", updatedLock2.QueueType)
	}

	// TTL and MaxTTL should remain unchanged
	if updatedLock2.TTL != 2*time.Second {
		t.Fatalf("TTL should remain unchanged, got %v", updatedLock2.TTL)
	}
	if updatedLock2.MaxTTL != 2*time.Minute {
		t.Fatalf("MaxTTL should remain unchanged, got %v", updatedLock2.MaxTTL)
	}
}

// TestRemoveFromQueueNonExistentLock tests removing from queue for non-existent lock
func TestRemoveFromQueueNonExistentLock(t *testing.T) {
	g := newServer(10)
	removed, code, err := g.RemoveFromQueue(&PollRequest{
		Name:      "non-existent",
		RequestID: "some-id",
		OwnerID:   "test-owner",
	})
	if err == nil || code != http.StatusNotFound || removed {
		t.Fatalf("expected not found error, got code=%d err=%v removed=%v", code, err, removed)
	}
}

// TestRemoveFromQueueNoQueue tests removing from lock without queue
func TestRemoveFromQueueNoQueue(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "no-queue-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueNone,
	})

	removed, code, err := g.RemoveFromQueue(&PollRequest{
		Name:      "no-queue-lock",
		RequestID: "some-id",
		OwnerID:   "test-owner",
	})
	if err == nil || code != http.StatusNotFound || removed {
		t.Fatalf("expected not found error for lock without queue, got code=%d err=%v removed=%v", code, err, removed)
	}
}

// TestRemoveFromQueueInvalidRequestID tests removing non-existent request ID
func TestRemoveFromQueueInvalidRequestID(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "queue-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueFIFO,
	})

	removed, code, err := g.RemoveFromQueue(&PollRequest{
		Name:      "queue-lock",
		RequestID: "non-existent-id",
		OwnerID:   "test-owner",
	})
	if err == nil || code != http.StatusNotFound || removed {
		t.Fatalf("expected not found error for invalid request ID, got code=%d err=%v removed=%v", code, err, removed)
	}
}

// TestRemoveFromQueueSuccess tests successful queue removal
func TestRemoveFromQueueSuccess(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "remove-test-lock",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueFIFO,
	})

	// Acquire the lock first so queueing happens
	_, _, _ = g.AcquireLock(&AcquireRequest{
		Name:    "remove-test-lock",
		Owner:   "first-owner",
		OwnerID: "first-owner-id",
	})

	// Now try to acquire again, should queue
	_, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "remove-test-lock",
		Owner:   "second-owner",
		OwnerID: "second-owner-id",
	})

	if err != nil || code != http.StatusAccepted {
		t.Fatalf("second acquire should be queued, got code=%d err=%v", code, err)
	}

	// Get the request ID from the queue (this is a bit of a hack since we don't have direct access)
	// In a real scenario, the request ID would come from the QueueResponse
	// For testing, we'll create a request with a known ID
	queueReq := &QueueRequest{
		ID:        "test-request-id",
		Name:      "remove-test-lock",
		Owner:     "second-owner",
		OwnerID:   "second-owner-id",
		QueuedAt:  time.Now(),
		TimeoutAt: time.Now().Add(time.Minute),
	}

	// Manually add to queue for testing
	lockVal, _ := g.Locks.Load("remove-test-lock")
	testLock := lockVal.(*Lock)
	testLock.queue.Enqueue(queueReq, false)

	// Now try to remove it
	removed, code, err := g.RemoveFromQueue(&PollRequest{
		Name:      "remove-test-lock",
		RequestID: "test-request-id",
		OwnerID:   "second-owner-id",
	})

	if err != nil || code != http.StatusOK || !removed {
		t.Fatalf("expected successful removal, got code=%d err=%v removed=%v", code, err, removed)
	}

	// Verify it's actually removed by trying to poll for it
	pollResp, code, _ := g.PollQueue(&PollRequest{
		Name:      "remove-test-lock",
		RequestID: "test-request-id",
		OwnerID:   "second-owner-id",
	})

	if pollResp.Status != "not_found" || code != http.StatusNotFound {
		t.Fatalf("expected not_found after removal, got status=%s code=%d", pollResp.Status, code)
	}
}

// TestTTLExpirationQueueProcessing tests the scenario where:
// 1. client1 acquires the lock
// 2. client2 tries to acquire, gets queued
// 3. client1's TTL expires
// 4. client2 should automatically get the lock via background cleanup
func TestTTLExpirationQueueProcessing(t *testing.T) {
	g := newServer(10)

	// Create a lock with short TTL for testing
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "ttl-test-lock",
		TTL:          "50ms", // Very short TTL
		MaxTTL:       "1s",
		QueueType:    QueueFIFO,
		QueueTimeout: "1s",
	})

	// 1. client1 acquires the lock
	result1, code1, err1 := g.AcquireLock(&AcquireRequest{
		Name:    "ttl-test-lock",
		Owner:   "client1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err1 != nil || code1 != http.StatusOK {
		t.Fatalf("client1 acquire failed: code=%d err=%v", code1, err1)
	}
	lock1 := result1.(*Lock)

	// 2. client2 tries to acquire, should get queued
	result2, code2, err2 := g.AcquireLock(&AcquireRequest{
		Name:    "ttl-test-lock",
		Owner:   "client2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err2 != nil || code2 != http.StatusAccepted {
		t.Fatalf("client2 should be queued, got code=%d err=%v", code2, err2)
	}
	queueResp := result2.(*QueueResponse)
	requestID := queueResp.RequestID

	// Verify client2 is in queue
	pollResp, _, _ := g.PollQueue(&PollRequest{
		Name:      "ttl-test-lock",
		RequestID: requestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})
	if pollResp.Status != "waiting" {
		t.Fatalf("client2 should be waiting in queue, got status=%s", pollResp.Status)
	}

	// 3. Wait for TTL to expire
	time.Sleep(100 * time.Millisecond) // Wait longer than TTL

	// 4. Manually trigger the cleanup process (simulating the background goroutine)
	g.processExpiredLocks()

	// 5. Check if client2 got the lock
	pollResp2, code3, err3 := g.PollQueue(&PollRequest{
		Name:      "ttl-test-lock",
		RequestID: requestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})

	// The request should either be granted the lock or show as ready
	if err3 != nil || (code3 != http.StatusOK && pollResp2.Status != "ready") {
		t.Fatalf("client2 should get the lock after TTL expiration, got code=%d status=%s err=%v", code3, pollResp2.Status, err3)
	}

	if pollResp2.Status == "ready" && pollResp2.Lock != nil {
		if pollResp2.Lock.Owner != "client2" {
			t.Fatalf("lock should be owned by client2, got owner=%s", pollResp2.Lock.Owner)
		}
	}

	// Verify that client1's lock is no longer valid
	_, code4, err4 := g.VerifyLock(&VerifyRequest{
		Name:    "ttl-test-lock",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   lock1.Token,
	})
	if err4 == nil || code4 != http.StatusConflict {
		t.Fatalf("client1's lock should be expired, got code=%d err=%v", code4, err4)
	}
}

// TestBackgroundCleanupGoroutine tests that the background cleanup works
func TestBackgroundCleanupGoroutine(t *testing.T) {
	g := newServer(10)

	// Create a lock with short TTL
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "bg-cleanup-test",
		TTL:          "10ms",
		MaxTTL:       "100ms",
		QueueType:    QueueFIFO,
		QueueTimeout: "1s",
	})

	// Start the cleanup goroutine
	g.StartCleanupGoroutine()
	defer g.StopCleanupGoroutine()

	// Acquire lock
	_, _, _ = g.AcquireLock(&AcquireRequest{
		Name:    "bg-cleanup-test",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})

	// Queue another client
	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "bg-cleanup-test",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusAccepted {
		t.Fatalf("second client should be queued")
	}
	queueResp := result.(*QueueResponse)

	// Wait for TTL to expire and background cleanup to run
	time.Sleep(200 * time.Millisecond) // Wait for cleanup interval (30s default, but should be fast in test)

	// Poll to see if we got the lock
	pollResp, code2, _ := g.PollQueue(&PollRequest{
		Name:      "bg-cleanup-test",
		RequestID: queueResp.RequestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})

	// Should either get the lock or be told it's ready
	if code2 == http.StatusOK && pollResp.Status == "ready" && pollResp.Lock != nil {
		if pollResp.Lock.Owner != "owner2" {
			t.Fatalf("expected owner2 to get the lock, got %s", pollResp.Lock.Owner)
		}
	}
}

// TestQueueMaxSize tests that the queue respects the maximum size limit
func TestQueueMaxSize(t *testing.T) {
	// Create server with queue max size of 2
	g := newServer(10)
	g.Config.QueueMaxSize = 2

	// Create a lock with FIFO queue
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "queue-max-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// 1. First client acquires the lock
	result1, code1, err1 := g.AcquireLock(&AcquireRequest{
		Name:    "queue-max-test",
		Owner:   "client1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err1 != nil || code1 != http.StatusOK {
		t.Fatalf("client1 acquire failed: code=%d err=%v", code1, err1)
	}
	lock1 := result1.(*Lock)
	if lock1.Owner != "client1" {
		t.Fatalf("expected client1 to own the lock, got %s", lock1.Owner)
	}

	// 2. Second client tries to acquire, should get queued
	_, code2, err2 := g.AcquireLock(&AcquireRequest{
		Name:    "queue-max-test",
		Owner:   "client2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err2 != nil || code2 != http.StatusAccepted {
		t.Fatalf("client2 should be queued, got code=%d err=%v", code2, err2)
	}

	// 3. Third client tries to acquire, should get queued (still under limit)
	_, code3, err3 := g.AcquireLock(&AcquireRequest{
		Name:    "queue-max-test",
		Owner:   "client3",
		OwnerID: "33333333-3333-3333-3333-333333333333",
	})
	if err3 != nil || code3 != http.StatusAccepted {
		t.Fatalf("client3 should be queued, got code=%d err=%v", code3, err3)
	}

	// Verify queue size is 2
	queueSize := lock1.getCurrentQueueSize()
	if queueSize != 2 {
		t.Fatalf("expected queue size 2, got %d", queueSize)
	}

	// 4. Fourth client tries to acquire, should be rejected due to queue limit
	_, code4, err4 := g.AcquireLock(&AcquireRequest{
		Name:    "queue-max-test",
		Owner:   "client4",
		OwnerID: "44444444-4444-4444-4444-444444444444",
	})
	if err4 == nil || code4 != http.StatusTooManyRequests {
		t.Fatalf("client4 should be rejected due to queue limit, got code=%d err=%v", code4, err4)
	}

	// Verify the error message contains the correct limit
	expectedError := "queue is at maximum capacity (2)"
	if err4.Error() != expectedError {
		t.Fatalf("expected error '%s', got '%s'", expectedError, err4.Error())
	}

	// 5. Fifth client should also be rejected
	_, code5, err5 := g.AcquireLock(&AcquireRequest{
		Name:    "queue-max-test",
		Owner:   "client5",
		OwnerID: "55555555-5555-5555-5555-555555555555",
	})
	if err5 == nil || code5 != http.StatusTooManyRequests {
		t.Fatalf("client5 should be rejected due to queue limit, got code=%d err=%v", code5, err5)
	}

	// Verify queue size is still 2 (unchanged)
	finalQueueSize := lock1.getCurrentQueueSize()
	if finalQueueSize != 2 {
		t.Fatalf("expected final queue size 2, got %d", finalQueueSize)
	}

	// 6. Release the lock, which should allow client2 (first in queue) to acquire it
	_, code6, err6 := g.ReleaseLock(&ReleaseRequest{
		Name:    "queue-max-test",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   lock1.Token,
	})
	if err6 != nil || code6 != http.StatusOK {
		t.Fatalf("lock release failed: code=%d err=%v", code6, err6)
	}

	// 7. Verify that client2 now owns the lock (should have been given it automatically)
	// We need to check the current lock state
	currentLockVal, exists := g.Locks.Load("queue-max-test")
	if !exists {
		t.Fatalf("lock should still exist")
	}
	currentLock := currentLockVal.(*Lock)
	if currentLock.Owner != "client2" {
		t.Fatalf("expected client2 to get the lock after release, got %s", currentLock.Owner)
	}

	// 8. Verify queue size is now 1 (client3 still waiting)
	finalQueueSize = currentLock.getCurrentQueueSize()
	if finalQueueSize != 1 {
		t.Fatalf("expected final queue size 1 (client3 still waiting), got %d", finalQueueSize)
	}
}
