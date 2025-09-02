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
