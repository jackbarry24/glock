package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test concurrent acquire attempts on the same lock
func TestConcurrentAcquire(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "1s", MaxTTL: "1m"})

	var successfulAcquires int64
	var wg sync.WaitGroup
	numClients := 10

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			ownerID := "11111111-1111-1111-1111-111111111111"
			if clientID > 0 {
				// Different owner IDs for different clients
				ownerID = "22222222-2222-2222-2222-222222222222"
			}
			result, code, err := g.AcquireLock(&AcquireRequest{
				Name:    "test",
				Owner:   "client",
				OwnerID: ownerID,
			})

			if err == nil && code == 200 {
				if lock, ok := result.(*Lock); ok && lock != nil {
					atomic.AddInt64(&successfulAcquires, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Only one client should successfully acquire the lock
	if successfulAcquires != 1 {
		t.Fatalf("expected 1 successful acquire, got %d", successfulAcquires)
	}
}

// Test concurrent refresh operations
func TestConcurrentRefresh(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "1s", MaxTTL: "1m"})
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "test", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)

	var successfulRefreshes int64
	var wg sync.WaitGroup
	numRefreshes := 5

	for i := 0; i < numRefreshes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, code, err := g.RefreshLock(&RefreshRequest{
				Name:    "test",
				OwnerID: "11111111-1111-1111-1111-111111111111",
				Token:   lock.Token,
			})

			if err == nil && code == 200 {
				atomic.AddInt64(&successfulRefreshes, 1)
			}
		}()
	}

	wg.Wait()

	// All refresh operations should succeed
	if successfulRefreshes != int64(numRefreshes) {
		t.Fatalf("expected %d successful refreshes, got %d", numRefreshes, successfulRefreshes)
	}
}

// Test race condition between acquire and release
func TestAcquireReleaseRace(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "1s", MaxTTL: "1m"})

	var wg sync.WaitGroup
	var acquireCount, releaseCount int64

	// Start multiple goroutines that acquire and release
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			ownerID := "11111111-1111-1111-1111-111111111111"
			if clientID > 0 {
				ownerID = "22222222-2222-2222-2222-222222222222"
			}

			// Try to acquire
			result, code, err := g.AcquireLock(&AcquireRequest{
				Name:    "test",
				Owner:   "client",
				OwnerID: ownerID,
			})

			if err == nil && code == 200 {
				if lock, ok := result.(*Lock); ok && lock != nil {
					atomic.AddInt64(&acquireCount, 1)

					// Small delay to simulate work
					time.Sleep(10 * time.Millisecond)

					// Try to release
					ok, code, err := g.ReleaseLock(&ReleaseRequest{
						Name:    "test",
						OwnerID: ownerID,
						Token:   lock.Token,
					})

					if err == nil && code == 200 && ok {
						atomic.AddInt64(&releaseCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// We should have at least one successful acquire/release cycle
	if acquireCount == 0 {
		t.Fatalf("expected at least 1 acquire, got %d", acquireCount)
	}
	if releaseCount == 0 {
		t.Fatalf("expected at least 1 release, got %d", releaseCount)
	}
}

// Test high contention scenario
func TestHighContentionScenario(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "100ms", MaxTTL: "1s"})

	var wg sync.WaitGroup
	var operations int64
	numWorkers := 20

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ownerID := "11111111-1111-1111-1111-111111111111"
			if workerID > 0 {
				ownerID = "22222222-2222-2222-2222-222222222222"
			}

			for j := 0; j < 10; j++ { // 10 operations per worker
				// Try to acquire
				result, code, err := g.AcquireLock(&AcquireRequest{
					Name:    "test",
					Owner:   "worker",
					OwnerID: ownerID,
				})

				if err == nil && code == 200 {
					if lock, ok := result.(*Lock); ok && lock != nil {
						atomic.AddInt64(&operations, 1)

						// Hold lock briefly
						time.Sleep(5 * time.Millisecond)

						// Try to release
						g.ReleaseLock(&ReleaseRequest{
							Name:    "test",
							OwnerID: ownerID,
							Token:   lock.Token,
						})
					}
				}

				// Small delay between attempts
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Should have some successful operations
	if operations == 0 {
		t.Fatalf("expected some successful operations, got %d", operations)
	}

	t.Logf("Completed %d successful acquire/release operations under high contention", operations)
}

// Test fencing token validation under concurrent access
func TestFencingTokenRaceCondition(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "1s", MaxTTL: "1m"})

	var wg sync.WaitGroup
	var staleTokenErrors int64

	// First, acquire the lock
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)
	originalToken := lock1.Token

	// Release it
	g.ReleaseLock(&ReleaseRequest{Name: "test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: originalToken})

	// Now start multiple goroutines that will try operations with the stale token
	numWorkers := 5
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Try refresh with stale token
			_, code, err := g.RefreshLock(&RefreshRequest{
				Name:    "test",
				OwnerID: "11111111-1111-1111-1111-111111111111",
				Token:   originalToken,
			})

			if err != nil && code == 409 { // Conflict due to stale token
				atomic.AddInt64(&staleTokenErrors, 1)
			}
		}()
	}

	wg.Wait()

	// All operations with stale token should fail
	if staleTokenErrors != int64(numWorkers) {
		t.Fatalf("expected %d stale token errors, got %d", numWorkers, staleTokenErrors)
	}
}

// Test TTL expiration race conditions
func TestTTLExpirationRace(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{Name: "test", TTL: "50ms", MaxTTL: "1s"})

	var wg sync.WaitGroup
	var expiredOperations int64

	// Acquire lock
	result, _, _ := g.AcquireLock(&AcquireRequest{Name: "test", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock := result.(*Lock)

	// Start goroutines that will try operations after TTL expires
	numWorkers := 3
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Wait for TTL to expire
			time.Sleep(100 * time.Millisecond)

			// Try refresh (should fail)
			_, code, err := g.RefreshLock(&RefreshRequest{
				Name:    "test",
				OwnerID: "11111111-1111-1111-1111-111111111111",
				Token:   lock.Token,
			})

			if err != nil && code == 409 {
				atomic.AddInt64(&expiredOperations, 1)
			}
		}()
	}

	wg.Wait()

	// All operations should fail due to expiration
	if expiredOperations != int64(numWorkers) {
		t.Fatalf("expected %d expired operations, got %d", numWorkers, expiredOperations)
	}
}

// Test multiple locks concurrent operations
func TestMultipleLocksConcurrency(t *testing.T) {
	g := newServer(10)

	// Create multiple locks
	lockNames := []string{"lock1", "lock2", "lock3"}
	for _, name := range lockNames {
		_, _, _ = g.CreateLock(&CreateRequest{Name: name, TTL: "1s", MaxTTL: "1m"})
	}

	var wg sync.WaitGroup
	var successfulOps int64
	numWorkers := 10

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for _, lockName := range lockNames {
				ownerID := "11111111-1111-1111-1111-111111111111"
				if workerID > 0 {
					ownerID = "22222222-2222-2222-2222-222222222222"
				}

				// Try acquire
				result, code, err := g.AcquireLock(&AcquireRequest{
					Name:    lockName,
					Owner:   "worker",
					OwnerID: ownerID,
				})

				if err == nil && code == 200 {
					if lock, ok := result.(*Lock); ok && lock != nil {
						atomic.AddInt64(&successfulOps, 1)

						// Try refresh
						_, code, err := g.RefreshLock(&RefreshRequest{
							Name:    lockName,
							OwnerID: ownerID,
							Token:   lock.Token,
						})

						if err == nil && code == 200 {
							atomic.AddInt64(&successfulOps, 1)
						}

						// Try release
						ok, code, err := g.ReleaseLock(&ReleaseRequest{
							Name:    lockName,
							OwnerID: ownerID,
							Token:   lock.Token,
						})

						if err == nil && code == 200 && ok {
							atomic.AddInt64(&successfulOps, 1)
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have successful operations across multiple locks
	if successfulOps == 0 {
		t.Fatalf("expected some successful operations on multiple locks, got %d", successfulOps)
	}

	t.Logf("Completed %d successful operations across %d locks with %d concurrent workers",
		successfulOps, len(lockNames), numWorkers)
}

// Test capacity limits under concurrent load
func TestCapacityConcurrentLoad(t *testing.T) {
	g := newServer(3) // Small capacity

	var wg sync.WaitGroup
	var createSuccess, createFail int64
	numWorkers := 10

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Use unique lock names to avoid conflicts
			lockName := fmt.Sprintf("lock%d", workerID)
			_, code, err := g.CreateLock(&CreateRequest{
				Name:   lockName,
				TTL:    "1s",
				MaxTTL: "1m",
			})

			if err == nil && code == 200 {
				atomic.AddInt64(&createSuccess, 1)
			} else if code == 429 { // Too Many Requests
				atomic.AddInt64(&createFail, 1)
			}
		}(i)
	}

	wg.Wait()

	// Should have exactly capacity number of successes
	if createSuccess != 3 {
		t.Fatalf("expected 3 successful creates (capacity limit), got %d", createSuccess)
	}

	// Should have some failures due to capacity
	if createFail == 0 {
		t.Fatalf("expected some create failures due to capacity, got %d", createFail)
	}
}

// Test FIFO queue behavior
func TestQueueFIFOBehavior(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "fifo-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "2s",
	})

	// Acquire first lock
	result1, code1, err1 := g.AcquireLock(&AcquireRequest{Name: "fifo-test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	if err1 != nil || code1 != 200 {
		t.Fatalf("first acquire should succeed")
	}
	lock1 := result1.(*Lock)

	// Try to acquire second lock - should queue
	result2, code2, err2 := g.AcquireLock(&AcquireRequest{Name: "fifo-test", Owner: "client2", OwnerID: "22222222-2222-2222-2222-222222222222"})
	if err2 != nil || code2 != 202 { // 202 Accepted for queued
		t.Fatalf("second acquire should be queued, got code=%d err=%v", code2, err2)
	}
	queueResp := result2.(*QueueResponse)

	// Try to acquire third lock - should queue after second
	result3, code3, err3 := g.AcquireLock(&AcquireRequest{Name: "fifo-test", Owner: "client3", OwnerID: "33333333-3333-3333-3333-333333333333"})
	if err3 != nil || code3 != 202 {
		t.Fatalf("third acquire should be queued")
	}
	queueResp3 := result3.(*QueueResponse)

	// Second should have position 1, third should have position 2
	if queueResp.Position != 1 {
		t.Fatalf("expected second request position 1, got %d", queueResp.Position)
	}
	if queueResp3.Position != 2 {
		t.Fatalf("expected third request position 2, got %d", queueResp3.Position)
	}

	// Poll second request - should be waiting
	pollResp2, code4, err4 := g.PollQueue(&PollRequest{
		Name:      "fifo-test",
		RequestID: queueResp.RequestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})
	if err4 != nil || code4 != 200 || pollResp2.Status != "waiting" {
		t.Fatalf("second request should be waiting, got status=%s", pollResp2.Status)
	}

	// Release first lock - second should get promoted
	ok, code5, err5 := g.ReleaseLock(&ReleaseRequest{Name: "fifo-test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock1.Token})
	if err5 != nil || code5 != 200 || !ok {
		t.Fatalf("release should succeed")
	}

	// Poll second request again - should now be ready
	pollResp2Ready, code6, err6 := g.PollQueue(&PollRequest{
		Name:      "fifo-test",
		RequestID: queueResp.RequestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})
	if err6 != nil || code6 != 200 || pollResp2Ready.Status != "ready" {
		t.Fatalf("second request should be ready after release, got status=%s", pollResp2Ready.Status)
	}
	if pollResp2Ready.Lock == nil {
		t.Fatalf("should have received lock in ready response")
	}
}

// Test LIFO queue behavior
func TestQueueLIFOBehavior(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "lifo-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueLIFO,
		QueueTimeout: "2s",
	})

	// Acquire first lock
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "lifo-test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)

	// Queue second request
	result2, _, _ := g.AcquireLock(&AcquireRequest{Name: "lifo-test", Owner: "client2", OwnerID: "22222222-2222-2222-2222-222222222222"})
	queueResp2 := result2.(*QueueResponse)

	// Queue third request - should be at front due to LIFO
	result3, _, _ := g.AcquireLock(&AcquireRequest{Name: "lifo-test", Owner: "client3", OwnerID: "33333333-3333-3333-3333-333333333333"})
	queueResp3 := result3.(*QueueResponse)

	// Debug: print positions
	t.Logf("Client2 position: %d, Client3 position: %d", queueResp2.Position, queueResp3.Position)

	// In LIFO, the third request should have position 1 (at front)
	if queueResp3.Position != 1 {
		t.Fatalf("expected third request position 1 (LIFO), got %d", queueResp3.Position)
	}
	// Note: Client2's stored position is from when it was queued, so it shows position 1
	// But the actual current position should be 2. Let's check the current position.
	lockVal, _ := g.Locks.Load("lifo-test")
	node := lockVal.(*Node)
	currentPos2 := node.Lock.queue.GetPosition(queueResp2.RequestID)
	if currentPos2 != 2 {
		t.Fatalf("expected current second request position 2, got %d", currentPos2)
	}

	// Release first lock - third request (most recent) should get the lock
	g.ReleaseLock(&ReleaseRequest{Name: "lifo-test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock1.Token})

	// Poll third request - should be ready
	pollResp3, _, _ := g.PollQueue(&PollRequest{
		Name:      "lifo-test",
		RequestID: queueResp3.RequestID,
		OwnerID:   "33333333-3333-3333-3333-333333333333",
	})
	if pollResp3.Status != "ready" {
		t.Fatalf("third request should be ready (LIFO), got status=%s", pollResp3.Status)
	}
}

// Test queue timeout behavior
func TestQueueTimeoutBehavior(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "timeout-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "50ms", // Very short timeout
	})

	// Acquire first lock
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "timeout-test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)

	// Queue second request
	result2, _, _ := g.AcquireLock(&AcquireRequest{Name: "timeout-test", Owner: "client2", OwnerID: "22222222-2222-2222-2222-222222222222"})
	queueResp2 := result2.(*QueueResponse)

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Poll should show expired
	pollResp, _, _ := g.PollQueue(&PollRequest{
		Name:      "timeout-test",
		RequestID: queueResp2.RequestID,
		OwnerID:   "22222222-2222-2222-2222-222222222222",
	})
	if pollResp.Status != "expired" {
		t.Fatalf("request should be expired, got status=%s", pollResp.Status)
	}

	// Release first lock - queue should be empty due to timeout
	g.ReleaseLock(&ReleaseRequest{Name: "timeout-test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock1.Token})

	// Try to acquire again - should succeed immediately
	result3, code3, err3 := g.AcquireLock(&AcquireRequest{Name: "timeout-test", Owner: "client3", OwnerID: "33333333-3333-3333-3333-333333333333"})
	if err3 != nil || code3 != 200 {
		t.Fatalf("third acquire should succeed after timeout, got code=%d err=%v", code3, err3)
	}
	lock3 := result3.(*Lock)
	if lock3 == nil {
		t.Fatalf("should have received lock")
	}
}

// Test queue with no queue behavior (should fail immediately)
func TestQueueNoneBehavior(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "none-test",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueNone, // Explicitly set to none
	})

	// Acquire first lock
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "none-test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)

	// Try to acquire second lock - should fail immediately
	_, code2, err2 := g.AcquireLock(&AcquireRequest{Name: "none-test", Owner: "client2", OwnerID: "22222222-2222-2222-2222-222222222222"})
	if err2 == nil || code2 != 409 { // 409 Conflict
		t.Fatalf("second acquire should fail with conflict, got code=%d err=%v", code2, err2)
	}

	// Clean up
	g.ReleaseLock(&ReleaseRequest{Name: "none-test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock1.Token})
}

// Test concurrent queue operations
func TestConcurrentQueueOperations(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "concurrent-queue",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "5s",
	})

	var wg sync.WaitGroup
	var successfulAcquires int64
	var queuedRequests int64
	numWorkers := 20

	// Start one goroutine to hold the lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, code, err := g.AcquireLock(&AcquireRequest{
			Name:    "concurrent-queue",
			Owner:   "holder",
			OwnerID: "holder1111-1111-1111-1111-111111111111",
		})
		if err == nil && code == 200 {
			if lock, ok := result.(*Lock); ok && lock != nil {
				atomic.AddInt64(&successfulAcquires, 1)
				time.Sleep(100 * time.Millisecond) // Hold lock briefly
				g.ReleaseLock(&ReleaseRequest{
					Name:    "concurrent-queue",
					OwnerID: "holder1111-1111-1111-1111-111111111111",
					Token:   lock.Token,
				})
			}
		}
	}()

	// Start multiple workers trying to acquire
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ownerID := fmt.Sprintf("worker%04d-1111-1111-1111-111111111111", workerID)

			result, code, err := g.AcquireLock(&AcquireRequest{
				Name:    "concurrent-queue",
				Owner:   fmt.Sprintf("worker%d", workerID),
				OwnerID: ownerID,
			})

			if err == nil && code == 200 {
				if lock, ok := result.(*Lock); ok && lock != nil {
					atomic.AddInt64(&successfulAcquires, 1)
					time.Sleep(10 * time.Millisecond)
					g.ReleaseLock(&ReleaseRequest{
						Name:    "concurrent-queue",
						OwnerID: ownerID,
						Token:   lock.Token,
					})
				}
			} else if code == 202 { // Queued
				atomic.AddInt64(&queuedRequests, 1)
				if queueResp, ok := result.(*QueueResponse); ok {
					// Poll until we get the lock or timeout
					timeout := time.After(2 * time.Second)
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()

					for {
						select {
						case <-timeout:
							return // Give up after timeout
						case <-ticker.C:
							pollResp, _, _ := g.PollQueue(&PollRequest{
								Name:      "concurrent-queue",
								RequestID: queueResp.RequestID,
								OwnerID:   ownerID,
							})

							if pollResp.Status == "ready" && pollResp.Lock != nil {
								atomic.AddInt64(&successfulAcquires, 1)
								time.Sleep(10 * time.Millisecond)
								g.ReleaseLock(&ReleaseRequest{
									Name:    "concurrent-queue",
									OwnerID: ownerID,
									Token:   pollResp.Lock.Token,
								})
								return
							} else if pollResp.Status == "expired" {
								return // Request expired
							}
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have at least some successful operations
	if successfulAcquires == 0 {
		t.Fatalf("expected some successful acquires, got %d", successfulAcquires)
	}

	t.Logf("Concurrent queue test: %d successful acquires, %d queued requests",
		successfulAcquires, queuedRequests)
}

// Test queue size limits and cleanup
func TestQueueSizeAndCleanup(t *testing.T) {
	g := newServer(10)
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "size-test",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "100ms", // Short timeout for cleanup test
	})

	// Acquire first lock
	result1, _, _ := g.AcquireLock(&AcquireRequest{Name: "size-test", Owner: "client1", OwnerID: "11111111-1111-1111-1111-111111111111"})
	lock1 := result1.(*Lock)

	// Queue multiple requests
	var queueRequests []*QueueResponse
	for i := 0; i < 5; i++ {
		ownerID := fmt.Sprintf("%04d1111-1111-1111-1111-111111111111", i+2)
		result, _, _ := g.AcquireLock(&AcquireRequest{
			Name:    "size-test",
			Owner:   fmt.Sprintf("client%d", i+2),
			OwnerID: ownerID,
		})
		if queueResp, ok := result.(*QueueResponse); ok {
			queueRequests = append(queueRequests, queueResp)
		}
	}

	// Should have 5 queued requests
	if len(queueRequests) != 5 {
		t.Fatalf("expected 5 queued requests, got %d", len(queueRequests))
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Poll first request - should be expired
	pollResp, _, _ := g.PollQueue(&PollRequest{
		Name:      "size-test",
		RequestID: queueRequests[0].RequestID,
		OwnerID:   "00021111-1111-1111-1111-111111111111",
	})
	if pollResp.Status != "expired" {
		t.Fatalf("first request should be expired, got status=%s", pollResp.Status)
	}

	// Release lock - since all requests are expired, lock should become available
	g.ReleaseLock(&ReleaseRequest{Name: "size-test", OwnerID: "11111111-1111-1111-1111-111111111111", Token: lock1.Token})

	// Poll second request - should be expired
	pollResp2, _, _ := g.PollQueue(&PollRequest{
		Name:      "size-test",
		RequestID: queueRequests[1].RequestID,
		OwnerID:   "00031111-1111-1111-1111-111111111111",
	})
	if pollResp2.Status != "expired" {
		t.Fatalf("second request should be expired, got status=%s", pollResp2.Status)
	}
}
