package core

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentParentChildAcquire tests race conditions when acquiring parent and child simultaneously
func TestConcurrentParentChildAcquire(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "child",
		Parent:       "parent",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	var wg sync.WaitGroup
	results := make(chan string, 2)

	// Try to acquire parent and child concurrently
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, code, _ := g.AcquireLock(&AcquireRequest{
			Name:    "parent",
			Owner:   "owner1",
			OwnerID: "11111111-1111-1111-1111-111111111111",
		})
		if code == 200 {
			results <- "parent-acquired"
		} else if code == 202 {
			results <- "parent-queued"
		}
	}()

	go func() {
		defer wg.Done()
		_, code, _ := g.AcquireLock(&AcquireRequest{
			Name:    "child",
			Owner:   "owner2",
			OwnerID: "22222222-2222-2222-2222-222222222222",
		})
		if code == 200 {
			results <- "child-acquired"
		} else if code == 202 {
			results <- "child-queued"
		}
	}()

	wg.Wait()
	close(results)

	// One should succeed, one should queue or vice versa
	acquired := 0
	queued := 0
	for result := range results {
		if result == "parent-acquired" || result == "child-acquired" {
			acquired++
		} else {
			queued++
		}
	}

	if acquired == 0 {
		t.Fatal("at least one lock should have been acquired")
	}
	if acquired == 2 {
		t.Fatal("both parent and child cannot be held simultaneously")
	}
}

// TestReleaseWhileAcquiring tests releasing a lock while another is trying to acquire
func TestReleaseWhileAcquiring(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "100ms",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "child",
		Parent:       "parent",
		TTL:          "100ms",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Acquire parent
	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	parentLock := result.(*Lock)

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Try to acquire child (will queue)
	go func() {
		defer wg.Done()
		g.AcquireLock(&AcquireRequest{
			Name:    "child",
			Owner:   "owner2",
			OwnerID: "22222222-2222-2222-2222-222222222222",
		})
	}()

	// Goroutine 2: Release parent after short delay
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		g.ReleaseLock(&ReleaseRequest{
			Name:    "parent",
			OwnerID: "11111111-1111-1111-1111-111111111111",
			Token:   parentLock.Token,
		})
	}()

	wg.Wait()

	// Child should eventually get the lock from queue
	time.Sleep(50 * time.Millisecond)
	nodeVal, _ := g.Locks.Load("child")
	childLock := nodeVal.(*Node).Lock
	childLock.mu.Lock()
	owner := childLock.OwnerID
	childLock.mu.Unlock()

	if owner != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("child should have been granted from queue, got owner: %s", owner)
	}
}

// TestCycleDetection tests that cycles in parent chains are prevented
func TestCycleDetection(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "A", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "B", Parent: "A", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "C", Parent: "B", TTL: "1s", MaxTTL: "1m"})

	// Try to make A a child of C (would create cycle: A -> B -> C -> A)
	_, code, err := g.UpdateLock(&UpdateRequest{
		Name:   "A",
		Parent: "C",
		TTL:    "1s",
		MaxTTL: "1m",
	})

	if err == nil || code != 400 {
		t.Fatalf("expected bad request when creating cycle, got code=%d err=%v", code, err)
	}
	if err.Error() != "cannot set parent: would create a cycle in lock hierarchy" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

// TestDeepHierarchy tests a deep hierarchy (10+ levels)
func TestDeepHierarchy(t *testing.T) {
	g := newServer(20)

	// Create 10-level hierarchy
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "level0",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	for i := 1; i < 10; i++ {
		parentName := "level" + string(rune('0'+i-1))
		lockName := "level" + string(rune('0'+i))
		_, _, err := g.CreateLock(&CreateRequest{
			Name:         lockName,
			Parent:       parentName,
			TTL:          "1s",
			MaxTTL:       "5m",
			QueueType:    QueueFIFO,
			QueueTimeout: "1m",
		})
		if err != nil {
			t.Fatalf("failed to create lock at level %d: %v", i, err)
		}
	}

	// Acquire root
	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "level0",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != 200 {
		t.Fatalf("failed to acquire root: code=%d err=%v", code, err)
	}
	rootLock := result.(*Lock)

	// Try to acquire leaf (should queue)
	_, code, _ = g.AcquireLock(&AcquireRequest{
		Name:    "level9",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code != 202 {
		t.Fatalf("expected leaf to queue when root held, got code=%d", code)
	}

	// Release root
	_, _, _ = g.ReleaseLock(&ReleaseRequest{
		Name:    "level0",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   rootLock.Token,
	})

	// Leaf should now be granted
	time.Sleep(10 * time.Millisecond)
	nodeVal, _ := g.Locks.Load("level9")
	leafLock := nodeVal.(*Node).Lock
	leafLock.mu.Lock()
	owner := leafLock.OwnerID
	leafLock.mu.Unlock()

	if owner != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("leaf should have been granted from queue, got owner: %s", owner)
	}
}

// TestParentWithMultipleChildrenQueued tests one parent with multiple children all queued
func TestParentWithMultipleChildrenQueued(t *testing.T) {
	g := newServer(20)

	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Create 5 children
	for i := 0; i < 5; i++ {
		childName := "child" + string(rune('0'+i))
		_, _, _ = g.CreateLock(&CreateRequest{
			Name:         childName,
			Parent:       "parent",
			TTL:          "1s",
			MaxTTL:       "5m",
			QueueType:    QueueFIFO,
			QueueTimeout: "1m",
		})
	}

	// Acquire parent
	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner-parent",
		OwnerID: "00000000-0000-0000-0000-000000000000",
	})
	parentLock := result.(*Lock)

	// Queue all children
	for i := 0; i < 5; i++ {
		childName := "child" + string(rune('0'+i))
		ownerID := "11111111-1111-1111-1111-11111111111" + string(rune('0'+i))
		_, code, _ := g.AcquireLock(&AcquireRequest{
			Name:    childName,
			Owner:   "owner" + string(rune('0'+i)),
			OwnerID: ownerID,
		})
		if code != 202 {
			t.Fatalf("child%d should have been queued, got code=%d", i, code)
		}
	}

	// Release parent
	_, _, _ = g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "00000000-0000-0000-0000-000000000000",
		Token:   parentLock.Token,
	})

	// All children should now be granted from their queues
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		childName := "child" + string(rune('0'+i))
		expectedOwner := "11111111-1111-1111-1111-11111111111" + string(rune('0'+i))

		nodeVal, _ := g.Locks.Load(childName)
		childLock := nodeVal.(*Node).Lock
		childLock.mu.Lock()
		owner := childLock.OwnerID
		childLock.mu.Unlock()

		if owner != expectedOwner {
			t.Fatalf("child%d should have been granted from queue, got owner: %s, expected: %s", i, owner, expectedOwner)
		}
	}
}

// TestMixedQueuePriority tests when parent and child both have queues
func TestMixedQueuePriority(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "child",
		Parent:       "parent",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Acquire parent
	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	parentLock := result.(*Lock)

	// Queue request for parent
	g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})

	// Queue request for child
	g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner3",
		OwnerID: "33333333-3333-3333-3333-333333333333",
	})

	// Release parent
	g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   parentLock.Token,
	})

	// Parent queue should get precedence (owner2 gets parent)
	time.Sleep(10 * time.Millisecond)
	nodeVal, _ := g.Locks.Load("parent")
	parentLockAfter := nodeVal.(*Node).Lock
	parentLockAfter.mu.Lock()
	parentOwner := parentLockAfter.OwnerID
	parentLockAfter.mu.Unlock()

	if parentOwner != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("parent should have been granted to owner2, got: %s", parentOwner)
	}

	// Child should still be queued (parent is now held by owner2)
	nodeVal, _ = g.Locks.Load("child")
	childLock := nodeVal.(*Node).Lock
	childLock.mu.Lock()
	childOwner := childLock.OwnerID
	queueSize := childLock.getCurrentQueueSize()
	childLock.mu.Unlock()

	if childOwner != "" {
		t.Fatalf("child should not be granted while parent is held, got owner: %s", childOwner)
	}
	if queueSize != 1 {
		t.Fatalf("child should still have 1 queued request, got: %d", queueSize)
	}
}

// TestConcurrentReleaseAndAcquire tests race between release and acquire
func TestConcurrentReleaseAndAcquire(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "lock1",
		TTL:          "1s",
		MaxTTL:       "5m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Run many iterations to catch race conditions
	for iter := 0; iter < 100; iter++ {
		// Acquire
		result, _, _ := g.AcquireLock(&AcquireRequest{
			Name:    "lock1",
			Owner:   "owner1",
			OwnerID: "11111111-1111-1111-1111-111111111111",
		})
		lock := result.(*Lock)

		var wg sync.WaitGroup
		wg.Add(2)

		// Concurrent release and acquire
		go func() {
			defer wg.Done()
			g.ReleaseLock(&ReleaseRequest{
				Name:    "lock1",
				OwnerID: "11111111-1111-1111-1111-111111111111",
				Token:   lock.Token,
			})
		}()

		go func() {
			defer wg.Done()
			g.AcquireLock(&AcquireRequest{
				Name:    "lock1",
				Owner:   "owner2",
				OwnerID: "22222222-2222-2222-2222-222222222222",
			})
		}()

		wg.Wait()

		// Clean up - release if held
		nodeVal, _ := g.Locks.Load("lock1")
		currentLock := nodeVal.(*Node).Lock
		currentLock.mu.Lock()
		if currentLock.OwnerID != "" {
			token := currentLock.Token
			owner := currentLock.OwnerID
			currentLock.mu.Unlock()
			g.ReleaseLock(&ReleaseRequest{
				Name:    "lock1",
				OwnerID: owner,
				Token:   token,
			})
		} else {
			currentLock.mu.Unlock()
		}
	}
}

// TestUpdateParentConcurrently tests concurrent updates to parent relationships
func TestUpdateParentConcurrently(t *testing.T) {
	g := newServer(20)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "A", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "B", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "C", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "target", TTL: "1s", MaxTTL: "1m"})

	var wg sync.WaitGroup
	wg.Add(3)

	// Try to set different parents concurrently
	go func() {
		defer wg.Done()
		g.UpdateLock(&UpdateRequest{
			Name:   "target",
			Parent: "A",
			TTL:    "1s",
			MaxTTL: "1m",
		})
	}()

	go func() {
		defer wg.Done()
		g.UpdateLock(&UpdateRequest{
			Name:   "target",
			Parent: "B",
			TTL:    "1s",
			MaxTTL: "1m",
		})
	}()

	go func() {
		defer wg.Done()
		g.UpdateLock(&UpdateRequest{
			Name:   "target",
			Parent: "C",
			TTL:    "1s",
			MaxTTL: "1m",
		})
	}()

	wg.Wait()

	// Verify target has a valid parent (one of A, B, or C)
	nodeVal, _ := g.Locks.Load("target")
	targetLock := nodeVal.(*Node).Lock
	targetLock.mu.Lock()
	parent := targetLock.Parent
	targetLock.mu.Unlock()

	if parent != "A" && parent != "B" && parent != "C" {
		t.Fatalf("target should have one of the parents, got: %s", parent)
	}
}
