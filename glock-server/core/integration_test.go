package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationBasicWorkflow(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(5 * time.Second),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	_, err = srv.Create("workflow-lock", lockCfg, nil, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	client1, err := srv.Acquire(AcquireReq{
		LockID:   "workflow-lock",
		ClientID: "client-1",
	})
	if err != nil || client1.State != "granted" {
		t.Fatalf("Client 1 should acquire lock: %v, state=%s", err, client1.State)
	}

	client2, err := srv.Acquire(AcquireReq{
		LockID:   "workflow-lock",
		ClientID: "client-2",
		Wait:     true,
	})
	if err != nil || client2.State != "queued" {
		t.Fatalf("Client 2 should be queued: %v, state=%s", err, client2.State)
	}

	queue, err := srv.QueueGet("workflow-lock", 10)
	if err != nil || len(queue.Queue) != 1 {
		t.Fatalf("Queue should have 1 entry: %v, len=%d", err, len(queue.Queue))
	}

	_, err = srv.Refresh(RefreshReq{
		LockID:   "workflow-lock",
		ClientID: "client-1",
		LeaseID:  client1.LeaseID,
		NewTTL:   Duration(10 * time.Second),
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	_, err = srv.Release(ReleaseReq{
		LockID:   "workflow-lock",
		ClientID: "client-1",
		LeaseID:  client1.LeaseID,
	})
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	status, err := srv.Status("workflow-lock")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Status.Owner == nil {
		t.Error("Lock should have been promoted to client-2")
	} else if status.Status.Owner.ClientID != "client-2" {
		t.Errorf("Owner should be client-2, got %s", status.Status.Owner.ClientID)
	}
}

func TestIntegrationExpiry(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           100 * time.Millisecond,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(100 * time.Millisecond),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("expiry-lock", lockCfg, nil, "")

	client1, err := srv.Acquire(AcquireReq{
		LockID:   "expiry-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(100 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	srv.Acquire(AcquireReq{
		LockID:   "expiry-lock",
		ClientID: "client-2",
		Wait:     true,
	})

	promoted := false
	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond)

		status, err := srv.Status("expiry-lock")
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}

		if status.Status.Owner != nil && status.Status.Owner.ClientID == "client-2" {
			promoted = true
			if status.Status.Owner.LeaseID == client1.LeaseID {
				t.Error("Old lease should not be valid")
			}
			break
		}
	}

	if !promoted {
		t.Error("Lock should have been promoted to client-2 after expiry")
	}
}

func TestIntegrationQueueTimeout(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(30 * time.Second),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("timeout-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "timeout-lock",
		ClientID: "owner",
	})

	srv.Acquire(AcquireReq{
		LockID:   "timeout-lock",
		ClientID: "waiter",
		Wait:     true,
		Timeout:  Duration(100 * time.Millisecond),
	})

	time.Sleep(50 * time.Millisecond)

	queue1, err := srv.QueueGet("timeout-lock", 10)
	if err != nil {
		t.Fatalf("QueueGet() error = %v", err)
	}
	if len(queue1.Queue) != 1 {
		t.Errorf("Queue should have 1 entry, got %d", len(queue1.Queue))
	}

	time.Sleep(100 * time.Millisecond)

	queue2, err := srv.QueueGet("timeout-lock", 10)
	if err != nil {
		t.Fatalf("QueueGet() error = %v", err)
	}
	if len(queue2.Queue) != 0 {
		t.Errorf("Queue should be empty after timeout, got %d entries", len(queue2.Queue))
	}
}

func TestIntegrationConcurrentAcquire(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            8,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 100,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(1 * time.Second),
		QueueCapacity: 100,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("concurrent-lock", lockCfg, nil, "")

	const numClients = 50
	var wg sync.WaitGroup
	results := make([]AcquireResp, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, _ := srv.Acquire(AcquireReq{
				LockID:   "concurrent-lock",
				ClientID: fmt.Sprintf("client-%d", idx),
				Wait:     true,
			})
			results[idx] = resp
		}(i)
	}

	wg.Wait()

	grantedCount := 0
	queuedCount := 0

	for _, resp := range results {
		if resp.State == "granted" {
			grantedCount++
		} else if resp.State == "queued" {
			queuedCount++
		}
	}

	if grantedCount != 1 {
		t.Errorf("Exactly 1 client should be granted, got %d", grantedCount)
	}

	if queuedCount+grantedCount != numClients {
		t.Errorf("All clients should be granted or queued, got %d granted + %d queued = %d, want %d",
			grantedCount, queuedCount, grantedCount+queuedCount, numClients)
	}
}

func TestIntegrationConcurrentReleaseAndAcquire(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(100 * time.Millisecond),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("race-lock", lockCfg, nil, "")

	const iterations = 10
	var successCount int32

	for i := 0; i < iterations; i++ {
		acq, err := srv.Acquire(AcquireReq{
			LockID:   "race-lock",
			ClientID: fmt.Sprintf("client-%d", i),
		})
		if err != nil {
			continue
		}

		go func(lease string, client string) {
			time.Sleep(10 * time.Millisecond)
			srv.Release(ReleaseReq{
				LockID:   "race-lock",
				ClientID: client,
				LeaseID:  lease,
			})
			atomic.AddInt32(&successCount, 1)
		}(acq.LeaseID, acq.OwnerClientID)

		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	if successCount != iterations {
		t.Errorf("Expected %d successful releases, got %d", iterations, successCount)
	}
}

func TestIntegrationMultipleLocks(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            8,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	const numLocks = 20
	var wg sync.WaitGroup

	for i := 0; i < numLocks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lockID := fmt.Sprintf("lock-%d", idx)

			srv.Create(lockID, lockCfg, nil, "")

			acq, err := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: fmt.Sprintf("client-%d", idx),
			})
			if err != nil {
				t.Errorf("Lock %s: Acquire error = %v", lockID, err)
				return
			}

			time.Sleep(10 * time.Millisecond)

			_, err = srv.Release(ReleaseReq{
				LockID:   lockID,
				ClientID: fmt.Sprintf("client-%d", idx),
				LeaseID:  acq.LeaseID,
			})
			if err != nil {
				t.Errorf("Lock %s: Release error = %v", lockID, err)
			}
		}(i)
	}

	wg.Wait()

	locks, err := srv.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(locks) != numLocks {
		t.Errorf("Expected %d locks, got %d", numLocks, len(locks))
	}
}

func TestIntegrationStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cfg := ServerConfig{
		NumShards:            16,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 50,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(100 * time.Millisecond),
		QueueCapacity: 50,
		QueueType:     QueueTypeFIFO,
	}

	const numLocks = 10
	const opsPerLock = 100

	for i := 0; i < numLocks; i++ {
		srv.Create(fmt.Sprintf("stress-lock-%d", i), lockCfg, nil, "")
	}

	var wg sync.WaitGroup
	var totalOps int32

	for lockIdx := 0; lockIdx < numLocks; lockIdx++ {
		for opIdx := 0; opIdx < opsPerLock; opIdx++ {
			wg.Add(1)
			go func(lIdx, oIdx int) {
				defer wg.Done()

				lockID := fmt.Sprintf("stress-lock-%d", lIdx)
				clientID := fmt.Sprintf("client-%d-%d", lIdx, oIdx)

				acq, err := srv.Acquire(AcquireReq{
					LockID:   lockID,
					ClientID: clientID,
					Wait:     false,
				})

				if err == nil && acq.State == "granted" {
					time.Sleep(5 * time.Millisecond)

					srv.Release(ReleaseReq{
						LockID:   lockID,
						ClientID: clientID,
						LeaseID:  acq.LeaseID,
					})

					atomic.AddInt32(&totalOps, 1)
				}
			}(lockIdx, opIdx)
		}
	}

	wg.Wait()

	if totalOps == 0 {
		t.Error("No operations succeeded")
	}

	t.Logf("Completed %d successful acquire/release operations", totalOps)
}

func TestIntegrationQueuePromotion(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           150 * time.Millisecond,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(150 * time.Millisecond),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("promo-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "promo-lock",
		ClientID: "client-0",
		LeaseTTL: Duration(150 * time.Millisecond),
	})

	expectedOrder := []string{"client-1", "client-2", "client-3"}
	for _, clientID := range expectedOrder {
		srv.Acquire(AcquireReq{
			LockID:   "promo-lock",
			ClientID: clientID,
			Wait:     true,
		})
	}

	for i, expectedClient := range expectedOrder {
		time.Sleep(180 * time.Millisecond)

		status, err := srv.Status("promo-lock")
		if err != nil {
			t.Fatalf("Iteration %d: Status() error = %v", i, err)
		}

		if status.Status.Owner == nil {
			t.Errorf("Iteration %d: Lock should have owner, queue may not have promoted yet", i)
			continue
		}

		if status.Status.Owner.ClientID != expectedClient {
			t.Errorf("Iteration %d: Owner = %s, want %s",
				i, status.Status.Owner.ClientID, expectedClient)
		}
	}
}

func TestIntegrationLIFOQueuePromotion(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           150 * time.Millisecond,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(150 * time.Millisecond),
		QueueCapacity: 10,
		QueueType:     QueueTypeLIFO,
	}

	srv.Create("lifo-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "lifo-lock",
		ClientID: "client-0",
		LeaseTTL: Duration(150 * time.Millisecond),
	})

	srv.Acquire(AcquireReq{
		LockID:   "lifo-lock",
		ClientID: "client-1",
		Wait:     true,
	})

	srv.Acquire(AcquireReq{
		LockID:   "lifo-lock",
		ClientID: "client-2",
		Wait:     true,
	})

	srv.Acquire(AcquireReq{
		LockID:   "lifo-lock",
		ClientID: "client-3",
		Wait:     true,
	})

	expectedOrder := []string{"client-3", "client-2", "client-1"}

	for i, expectedClient := range expectedOrder {
		time.Sleep(180 * time.Millisecond)

		status, err := srv.Status("lifo-lock")
		if err != nil {
			t.Fatalf("Iteration %d: Status() error = %v", i, err)
		}

		if status.Status.Owner == nil {
			t.Errorf("Iteration %d: Lock should have owner, queue may not have promoted yet", i)
			continue
		}

		if status.Status.Owner.ClientID != expectedClient {
			t.Errorf("Iteration %d (LIFO): Owner = %s, want %s",
				i, status.Status.Owner.ClientID, expectedClient)
		}
	}
}

func TestIntegrationMetricsAccuracy(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 3,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("metrics-lock", lockCfg, nil, "")

	acq, _ := srv.Acquire(AcquireReq{
		LockID:   "metrics-lock",
		ClientID: "client-1",
	})

	for i := 0; i < 3; i++ {
		srv.Acquire(AcquireReq{
			LockID:   "metrics-lock",
			ClientID: fmt.Sprintf("queued-%d", i),
			Wait:     true,
		})
	}

	srv.Acquire(AcquireReq{
		LockID:   "metrics-lock",
		ClientID: "rejected",
		Wait:     true,
	})

	srv.Refresh(RefreshReq{
		LockID:   "metrics-lock",
		ClientID: "client-1",
		LeaseID:  acq.LeaseID,
		NewTTL:   Duration(15 * time.Second),
	})

	srv.Release(ReleaseReq{
		LockID:   "metrics-lock",
		ClientID: "client-1",
		LeaseID:  acq.LeaseID,
	})

	time.Sleep(50 * time.Millisecond)

	metrics, err := srv.Metrics("metrics-lock")
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}

	m := metrics.Metrics

	if m.TotalAcquires != 5 {
		t.Errorf("TotalAcquires = %d, want 5", m.TotalAcquires)
	}

	if m.SuccessAcquires != 1 {
		t.Errorf("SuccessAcquires = %d, want 1", m.SuccessAcquires)
	}

	if m.FailedAcquires != 1 {
		t.Errorf("FailedAcquires = %d, want 1 (queue full)", m.FailedAcquires)
	}

	if m.TotalQueued != 3 {
		t.Errorf("TotalQueued = %d, want 3", m.TotalQueued)
	}

	if m.TotalRefreshes != 1 {
		t.Errorf("TotalRefreshes = %d, want 1", m.TotalRefreshes)
	}

	if m.TotalReleases != 1 {
		t.Errorf("TotalReleases = %d, want 1", m.TotalReleases)
	}

	if m.OwnerChanges < 2 {
		t.Errorf("OwnerChanges = %d, want at least 2 (initial + promotion)", m.OwnerChanges)
	}
}

func TestIntegrationFencingTokens(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(5 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	_, err = srv.Create("fence-lock", lockCfg, nil, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	t.Run("first acquire gets fence token 1", func(t *testing.T) {
		resp, err := srv.Acquire(AcquireReq{
			LockID:   "fence-lock",
			ClientID: "client-1",
		})
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}
		if resp.FenceToken != 1 {
			t.Errorf("FenceToken = %d, want 1", resp.FenceToken)
		}
	})

	t.Run("refresh returns same fence token", func(t *testing.T) {
		status, err := srv.Status("fence-lock")
		if err != nil || status.Status.Owner == nil {
			t.Fatalf("Status() error = %v or owner is nil", err)
		}

		refresh, err := srv.Refresh(RefreshReq{
			LockID:   "fence-lock",
			ClientID: "client-1",
			LeaseID:  status.Status.Owner.LeaseID,
			NewTTL:   Duration(10 * time.Second),
		})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if refresh.FenceToken != 1 {
			t.Errorf("FenceToken = %d, want 1 (should not change on refresh)", refresh.FenceToken)
		}
	})

	t.Run("release and reacquire increments fence token", func(t *testing.T) {
		status, err := srv.Status("fence-lock")
		if err != nil || status.Status.Owner == nil {
			t.Fatalf("Status() error = %v or owner is nil", err)
		}

		_, err = srv.Release(ReleaseReq{
			LockID:   "fence-lock",
			ClientID: "client-1",
			LeaseID:  status.Status.Owner.LeaseID,
		})
		if err != nil {
			t.Fatalf("Release() error = %v", err)
		}

		resp, err := srv.Acquire(AcquireReq{
			LockID:   "fence-lock",
			ClientID: "client-2",
		})
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}
		if resp.FenceToken != 2 {
			t.Errorf("FenceToken = %d, want 2 (should increment on new owner)", resp.FenceToken)
		}
	})

	t.Run("queue promotion increments fence token", func(t *testing.T) {
		_, err := srv.Acquire(AcquireReq{
			LockID:   "fence-lock",
			ClientID: "client-3",
			Wait:     true,
			Timeout:  Duration(1 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}

		status, err := srv.Status("fence-lock")
		if err != nil || status.Status.Owner == nil {
			t.Fatalf("Status() error = %v or owner is nil", err)
		}

		_, err = srv.Release(ReleaseReq{
			LockID:   "fence-lock",
			ClientID: "client-2",
			LeaseID:  status.Status.Owner.LeaseID,
		})
		if err != nil {
			t.Fatalf("Release() error = %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		status, err = srv.Status("fence-lock")
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}

		if status.Status.Owner == nil {
			t.Fatal("Owner should be promoted from queue")
		}
		if status.Status.Owner.ClientID != "client-3" {
			t.Errorf("Owner ClientID = %s, want client-3", status.Status.Owner.ClientID)
		}
		if status.Status.Owner.FenceToken != 3 {
			t.Errorf("FenceToken = %d, want 3 (should increment on queue promotion)", status.Status.Owner.FenceToken)
		}
	})
}

func TestIntegrationFencingTokensConcurrent(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 0,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(100 * time.Millisecond),
		QueueCapacity: 0,
		QueueType:     QueueTypeFIFO,
	}

	numLocks := 10
	clientsPerLock := 20

	for i := 0; i < numLocks; i++ {
		lockID := fmt.Sprintf("fence-lock-%d", i)
		_, err = srv.Create(lockID, lockCfg, nil, "")
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	var wg sync.WaitGroup
	seenTokens := make([]map[int64]bool, numLocks)
	for i := range seenTokens {
		seenTokens[i] = make(map[int64]bool)
	}
	var tokenMutexes []*sync.Mutex
	for i := 0; i < numLocks; i++ {
		tokenMutexes = append(tokenMutexes, &sync.Mutex{})
	}

	for lockIdx := 0; lockIdx < numLocks; lockIdx++ {
		for clientIdx := 0; clientIdx < clientsPerLock; clientIdx++ {
			wg.Add(1)
			go func(lid, cid int) {
				defer wg.Done()

				lockID := fmt.Sprintf("fence-lock-%d", lid)
				clientID := fmt.Sprintf("client-%d-%d", lid, cid)

				for attempt := 0; attempt < 50; attempt++ {
					resp, err := srv.Acquire(AcquireReq{
						LockID:   lockID,
						ClientID: clientID,
						LeaseTTL: Duration(100 * time.Millisecond),
					})
					if err != nil || resp.State != "granted" {
						time.Sleep(10 * time.Millisecond)
						continue
					}

					if resp.FenceToken <= 0 {
						t.Errorf("Invalid fence token: %d", resp.FenceToken)
					}

					tokenMutexes[lid].Lock()
					if seenTokens[lid][resp.FenceToken] {
						t.Errorf("Duplicate fence token %d for lock %s", resp.FenceToken, lockID)
					}
					seenTokens[lid][resp.FenceToken] = true
					tokenMutexes[lid].Unlock()

					time.Sleep(50 * time.Millisecond)

					_, _ = srv.Release(ReleaseReq{
						LockID:   lockID,
						ClientID: clientID,
						LeaseID:  resp.LeaseID,
					})

					break
				}
			}(lockIdx, clientIdx)
		}
	}

	wg.Wait()

	for i := 0; i < numLocks; i++ {
		lockID := fmt.Sprintf("fence-lock-%d", i)
		t.Logf("Lock %s: %d unique fence tokens seen", lockID, len(seenTokens[i]))
	}
}

func TestIntegrationFencingTokensExpiry(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           100 * time.Millisecond,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(100 * time.Millisecond),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("expiry-fence-lock", lockCfg, nil, "")

	client1, err := srv.Acquire(AcquireReq{
		LockID:   "expiry-fence-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(100 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if client1.FenceToken != 1 {
		t.Errorf("FenceToken = %d, want 1", client1.FenceToken)
	}

	srv.Acquire(AcquireReq{
		LockID:   "expiry-fence-lock",
		ClientID: "client-2",
		Wait:     true,
	})

	promoted := false
	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond)

		status, err := srv.Status("expiry-fence-lock")
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}

		if status.Status.Owner != nil && status.Status.Owner.ClientID == "client-2" {
			promoted = true
			if status.Status.Owner.LeaseID == client1.LeaseID {
				t.Error("Old lease should not be valid")
			}
			if status.Status.Owner.FenceToken != 2 {
				t.Errorf("FenceToken = %d, want 2 (should increment after expiry promotion)", status.Status.Owner.FenceToken)
			}
			break
		}
	}

	if !promoted {
		t.Error("Lock should have been promoted to client-2 after expiry")
	}
}

func TestIntegrationFencingTokensMonotonicity(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 0,
		DefaultQueueTimeout:  5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(5 * time.Second),
		QueueCapacity: 0,
		QueueType:     QueueTypeFIFO,
	}

	_, err = srv.Create("monotonic-lock", lockCfg, nil, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var lastToken int64
	for i := 0; i < 20; i++ {
		clientID := fmt.Sprintf("client-%d", i)

		resp, err := srv.Acquire(AcquireReq{
			LockID:   "monotonic-lock",
			ClientID: clientID,
		})
		if err != nil || resp.State != "granted" {
			t.Fatalf("Acquire %d: error = %v, state = %s", i, err, resp.State)
		}

		if resp.FenceToken <= lastToken {
			t.Errorf("Acquire %d: FenceToken = %d, want > %d (not monotonically increasing)", i, resp.FenceToken, lastToken)
		}
		lastToken = resp.FenceToken

		status, err := srv.Status("monotonic-lock")
		if err != nil || status.Status.Owner == nil {
			t.Fatalf("Status() error = %v or owner is nil", err)
		}

		_, err = srv.Release(ReleaseReq{
			LockID:   "monotonic-lock",
			ClientID: clientID,
			LeaseID:  status.Status.Owner.LeaseID,
		})
		if err != nil {
			t.Fatalf("Release() error = %v", err)
		}
	}

	if lastToken != 20 {
		t.Errorf("Final fence token = %d, want 20", lastToken)
	}
}
