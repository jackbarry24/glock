package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRaceConcurrentAcquireSameLock(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 100,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("race-lock", lockCfg, nil, "")

	const numGoroutines = 100
	var wg sync.WaitGroup
	var grantedCount int32
	granted := make([]string, 0)
	var grantedMux sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clientID := fmt.Sprintf("client-%d", idx)

			resp, err := srv.Acquire(AcquireReq{
				LockID:   "race-lock",
				ClientID: clientID,
				Wait:     false,
			})

			if err == nil && resp.State == "granted" {
				atomic.AddInt32(&grantedCount, 1)
				grantedMux.Lock()
				granted = append(granted, clientID)
				grantedMux.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if grantedCount != 1 {
		t.Errorf("Expected exactly 1 grant, got %d. Granted to: %v", grantedCount, granted)
	}
}

func TestRaceConcurrentRefreshAndRelease(t *testing.T) {
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("refresh-release-lock", lockCfg, nil, "")

	acq, err := srv.Acquire(AcquireReq{
		LockID:   "refresh-release-lock",
		ClientID: "client-1",
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	var wg sync.WaitGroup
	var refreshSuccess, releaseSuccess int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.Refresh(RefreshReq{
				LockID:   "refresh-release-lock",
				ClientID: "client-1",
				LeaseID:  acq.LeaseID,
				NewTTL:   Duration(15 * time.Second),
			})
			if err == nil {
				atomic.AddInt32(&refreshSuccess, 1)
			}
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.Release(ReleaseReq{
				LockID:   "refresh-release-lock",
				ClientID: "client-1",
				LeaseID:  acq.LeaseID,
			})
			if err == nil {
				atomic.AddInt32(&releaseSuccess, 1)
			}
		}()
	}

	wg.Wait()

	total := refreshSuccess + releaseSuccess
	if total == 0 {
		t.Error("Either refresh or release should succeed")
	}

	if releaseSuccess > 1 {
		t.Errorf("Release should succeed at most once, got %d", releaseSuccess)
	}
}

func TestRaceConcurrentCreateSameLock(t *testing.T) {
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	var createdCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := srv.Create("race-create-lock", lockCfg, nil, "")
			if err == nil && resp.Created {
				atomic.AddInt32(&createdCount, 1)
			}
		}()
	}

	wg.Wait()

	if createdCount != 1 {
		t.Errorf("Expected exactly 1 successful create, got %d", createdCount)
	}

	status, err := srv.Status("race-create-lock")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Status.LockID != "race-create-lock" {
		t.Error("Lock should exist")
	}
}

func TestRaceConcurrentDeleteAndAcquire(t *testing.T) {
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	for iter := 0; iter < 10; iter++ {
		lockID := fmt.Sprintf("delete-acquire-lock-%d", iter)
		srv.Create(lockID, lockCfg, nil, "")

		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				srv.Acquire(AcquireReq{
					LockID:   lockID,
					ClientID: fmt.Sprintf("client-%d", idx),
				})
			}(i)
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srv.Delete(lockID)
			}()
		}

		wg.Wait()
	}
}

func TestRaceConcurrentStatusChecks(t *testing.T) {
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("status-lock", lockCfg, nil, "")

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Status("status-lock")
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			acq, err := srv.Acquire(AcquireReq{
				LockID:   "status-lock",
				ClientID: fmt.Sprintf("client-%d", idx),
				Wait:     false,
			})
			if err == nil && acq.State == "granted" {
				time.Sleep(5 * time.Millisecond)
				srv.Release(ReleaseReq{
					LockID:   "status-lock",
					ClientID: fmt.Sprintf("client-%d", idx),
					LeaseID:  acq.LeaseID,
				})
			}
		}(i)
	}

	wg.Wait()
}

func TestRaceConcurrentQueueOperations(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 50,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("queue-race-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "queue-race-lock",
		ClientID: "owner",
	})

	waitIDs := make([]string, 20)
	for i := 0; i < 20; i++ {
		resp, _ := srv.Acquire(AcquireReq{
			LockID:   "queue-race-lock",
			ClientID: fmt.Sprintf("waiter-%d", i),
			Wait:     true,
		})
		if resp.State == "queued" {
			waitIDs[i] = resp.WaitID
		}
	}

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.QueueGet("queue-race-lock", 50)
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx < len(waitIDs) && waitIDs[idx] != "" {
				srv.QueueDel("queue-race-lock", waitIDs[idx], fmt.Sprintf("waiter-%d", idx))
			}
		}(i)
	}

	wg.Wait()
}

func TestRaceExpiryWithRefresh(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           5 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           50 * time.Millisecond,
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
		DefaultTTL:    Duration(50 * time.Millisecond),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("expiry-race-lock", lockCfg, nil, "")

	acq, err := srv.Acquire(AcquireReq{
		LockID:   "expiry-race-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(50 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	done := make(chan bool)
	var refreshCount int32

	go func() {
		for i := 0; i < 20; i++ {
			_, err := srv.Refresh(RefreshReq{
				LockID:   "expiry-race-lock",
				ClientID: "client-1",
				LeaseID:  acq.LeaseID,
				NewTTL:   Duration(50 * time.Millisecond),
			})
			if err == nil {
				atomic.AddInt32(&refreshCount, 1)
			}
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	<-done

	t.Logf("Successful refreshes: %d out of 20 attempts", refreshCount)
}

func TestRaceConcurrentReleaseQueuePromotion(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
		ExpiryTick:           10 * time.Millisecond,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 20,
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
		QueueCapacity: 20,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("promotion-race-lock", lockCfg, nil, "")

	acq, err := srv.Acquire(AcquireReq{
		LockID:   "promotion-race-lock",
		ClientID: "owner",
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	for i := 0; i < 10; i++ {
		srv.Acquire(AcquireReq{
			LockID:   "promotion-race-lock",
			ClientID: fmt.Sprintf("waiter-%d", i),
			Wait:     true,
		})
	}

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Release(ReleaseReq{
				LockID:   "promotion-race-lock",
				ClientID: "owner",
				LeaseID:  acq.LeaseID,
			})
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Status("promotion-race-lock")
		}()
	}

	wg.Wait()

	time.Sleep(50 * time.Millisecond)

	status, err := srv.Status("promotion-race-lock")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Status.Owner == nil {
		t.Error("Lock should have owner from queue promotion")
	}
}

func TestRaceMultipleShardsConcurrent(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            16,
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
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	const numLocks = 100
	const numOpsPerLock = 10

	var wg sync.WaitGroup

	for lockIdx := 0; lockIdx < numLocks; lockIdx++ {
		lockID := fmt.Sprintf("shard-race-lock-%d", lockIdx)
		srv.Create(lockID, lockCfg, nil, "")

		for opIdx := 0; opIdx < numOpsPerLock; opIdx++ {
			wg.Add(1)
			go func(lID string, oIdx int) {
				defer wg.Done()

				clientID := fmt.Sprintf("client-%s-%d", lID, oIdx)

				acq, err := srv.Acquire(AcquireReq{
					LockID:   lID,
					ClientID: clientID,
					Wait:     false,
				})

				if err == nil && acq.State == "granted" {
					time.Sleep(1 * time.Millisecond)

					srv.Release(ReleaseReq{
						LockID:   lID,
						ClientID: clientID,
						LeaseID:  acq.LeaseID,
					})
				}
			}(lockID, opIdx)
		}
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

func TestRaceMetricsConsistency(t *testing.T) {
	cfg := ServerConfig{
		NumShards:            4,
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
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 50,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("metrics-race-lock", lockCfg, nil, "")

	var wg sync.WaitGroup
	const numOps = 100

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			srv.Acquire(AcquireReq{
				LockID:   "metrics-race-lock",
				ClientID: fmt.Sprintf("client-%d", idx),
				Wait:     true,
			})
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Metrics("metrics-race-lock")
		}()
	}

	wg.Wait()

	metrics, err := srv.Metrics("metrics-race-lock")
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}

	if metrics.Metrics.TotalAcquires != numOps {
		t.Errorf("TotalAcquires = %d, want %d", metrics.Metrics.TotalAcquires, numOps)
	}

	total := metrics.Metrics.SuccessAcquires + metrics.Metrics.FailedAcquires + metrics.Metrics.TotalQueued
	if total != numOps {
		t.Errorf("Sum of success+failed+queued (%d) != TotalAcquires (%d)",
			total, metrics.Metrics.TotalAcquires)
	}
}

func TestRaceServerStopDuringOperations(t *testing.T) {
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

	lockCfg := LockConfig{
		MaxTTL:        Duration(1 * time.Minute),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 10,
		QueueType:     QueueTypeFIFO,
	}

	srv.Create("stop-race-lock", lockCfg, nil, "")

	for i := 0; i < 20; i++ {
		go func(idx int) {
			srv.Acquire(AcquireReq{
				LockID:   "stop-race-lock",
				ClientID: fmt.Sprintf("client-%d", idx),
			})
		}(i)
	}

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Stop(ctx)

	if ctx.Err() != nil {
		t.Error("Server should stop before context timeout")
	}
}
