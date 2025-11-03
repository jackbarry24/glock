package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// benchDefaultLockConfig builds a LockConfig from the server's defaults.
func benchDefaultLockConfig(srv *Server) LockConfig {
	return LockConfig{
		MaxTTL:         Duration(srv.cfg.DefaultMaxTTL),
		DefaultTTL:     Duration(srv.cfg.DefaultTTL),
		MaxQueue:       srv.cfg.DefaultQueue,
		QueueType:      QueueTypeFIFO,
		DefaultTimeout: Duration(srv.cfg.DefaultQueueTimeout),
	}
}

// ensureLockCreated creates a lock if it doesn't already exist.
func ensureLockCreated(b *testing.B, srv *Server, lockID string) {
	cfg := benchDefaultLockConfig(srv)
	if _, err := srv.Create(lockID, cfg, nil, ""); err != nil && err != ErrLockExists {
		b.Fatalf("Create failed for %s: %v", lockID, err)
	}
}

// setupBenchServer creates and starts a server with optimized config for benchmarking
func setupBenchServer(b *testing.B) *Server {
	cfg := ServerConfig{
		NumShards:           16,
		ExpiryTick:          100 * time.Millisecond,
		Capacity:            0,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        100,
		DefaultQueueTimeout: 5 * time.Minute,
		LockPoolSize:        200,
		LockPoolMinSize:     100,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		b.Fatalf("Failed to create server: %v", err)
	}

	srv.Start()

	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Stop(ctx)
	})

	return srv
}

// BenchmarkAcquireRelease_Uncontended tests acquire/release on different locks (no contention)
func BenchmarkAcquireRelease_Uncontended(b *testing.B) {
	srv := setupBenchServer(b)
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()
		// Pre-create a pool of unique locks for this worker outside the timer.
		const precreate = 1024
		ids := make([]string, precreate)
		b.StopTimer()
		for i := 0; i < precreate; i++ {
			ids[i] = fmt.Sprintf("lock-%s-%d", clientID, i)
			ensureLockCreated(b, srv, ids[i])
		}
		b.StartTimer()

		i := 0
		for pb.Next() {
			lockID := ids[i%precreate]
			i++

			resp, err := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseTTL: Duration(5 * time.Second),
			})
			if err != nil {
				b.Fatalf("Acquire failed: %v", err)
			}
			if resp.State != "granted" {
				b.Fatalf("Expected granted, got %s", resp.State)
			}

			_, err = srv.Release(ReleaseReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseID:  resp.LeaseID,
			})
			if err != nil {
				b.Fatalf("Release failed: %v", err)
			}
		}
	})
}

// BenchmarkAcquireRelease_Contended tests acquire/release on the same lock (high contention)
func BenchmarkAcquireRelease_Contended(b *testing.B) {
	srv := setupBenchServer(b)
	lockID := "contended-lock"
	// Ensure the lock exists before timing.
	ensureLockCreated(b, srv, lockID)
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()

		for pb.Next() {
			resp, _ := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseTTL: Duration(5 * time.Second),
			})

			if resp.State == "granted" {
				srv.Release(ReleaseReq{
					LockID:   lockID,
					ClientID: clientID,
					LeaseID:  resp.LeaseID,
				})
			}
		}
	})
}

// BenchmarkAcquireRelease_SameShard tests multiple locks in the same shard
func BenchmarkAcquireRelease_SameShard(b *testing.B) {
	srv := setupBenchServer(b)

	// Find a lock prefix that consistently hashes to the same shard
	shardID := srv.shardOf("lock-0").id
	var lockPrefix string
	for i := 0; i < 1000; i++ {
		candidate := fmt.Sprintf("shard%d-", i)
		if srv.shardOf(candidate+"test").id == shardID {
			lockPrefix = candidate
			break
		}
	}

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()
		// Pre-create a pool of locks on the same shard for this worker.
		const precreate = 1024
		ids := make([]string, precreate)
		b.StopTimer()
		for i := 0; i < precreate; i++ {
			ids[i] = fmt.Sprintf("%s%s-%d", lockPrefix, clientID, i)
			ensureLockCreated(b, srv, ids[i])
		}
		b.StartTimer()
		i := 0

		for pb.Next() {
			lockID := ids[i%precreate]
			i++

			resp, err := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseTTL: Duration(5 * time.Second),
			})
			if err != nil {
				b.Fatalf("Acquire failed: %v", err)
			}
			if resp.State != "granted" {
				b.Fatalf("Expected granted, got %s", resp.State)
			}

			_, err = srv.Release(ReleaseReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseID:  resp.LeaseID,
			})
			if err != nil {
				b.Fatalf("Release failed: %v", err)
			}
		}
	})
}

// BenchmarkRefresh tests refresh operations under load
func BenchmarkRefresh(b *testing.B) {
	srv := setupBenchServer(b)

	// Pre-create locks with leases
	numLocks := 100
	locks := make([]struct {
		lockID   string
		clientID string
		leaseID  string
	}, numLocks)

	for i := 0; i < numLocks; i++ {
		clientID := uuid.New().String()
		lockID := fmt.Sprintf("refresh-lock-%d", i)
		// Create then Acquire to set up leases
		ensureLockCreated(b, srv, lockID)
		resp, err := srv.Acquire(AcquireReq{
			LockID:   lockID,
			ClientID: clientID,
			LeaseTTL: Duration(5 * time.Minute),
		})
		if err != nil {
			b.Fatalf("Setup acquire failed: %v", err)
		}

		locks[i].lockID = lockID
		locks[i].clientID = clientID
		locks[i].leaseID = resp.LeaseID
	}

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lock := locks[i%numLocks]
			i++

			_, err := srv.Refresh(RefreshReq{
				LockID:   lock.lockID,
				ClientID: lock.clientID,
				LeaseID:  lock.leaseID,
				NewTTL:   Duration(5 * time.Minute),
			})
			if err != nil {
				b.Fatalf("Refresh failed: %v", err)
			}
		}
	})
}

// BenchmarkMixedWorkload tests realistic mix of operations
func BenchmarkMixedWorkload(b *testing.B) {
	srv := setupBenchServer(b)
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()
		// Pre-create a pool of locks for this worker to avoid creation during timing.
		const precreate = 1024
		ids := make([]string, precreate)
		b.StopTimer()
		for i := 0; i < precreate; i++ {
			ids[i] = fmt.Sprintf("mixed-lock-%s-%d", clientID, i)
			ensureLockCreated(b, srv, ids[i])
		}
		b.StartTimer()
		i := 0

		for pb.Next() {
			lockID := ids[i%precreate]
			i++

			// Acquire
			resp, err := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseTTL: Duration(5 * time.Second),
			})
			if err != nil {
				b.Fatalf("Acquire failed: %v", err)
			}

			// Refresh twice
			for i := 0; i < 2; i++ {
				_, err = srv.Refresh(RefreshReq{
					LockID:   lockID,
					ClientID: clientID,
					LeaseID:  resp.LeaseID,
					NewTTL:   Duration(5 * time.Second),
				})
				if err != nil {
					b.Fatalf("Refresh failed: %v", err)
				}
			}

			// Status check
			_, err = srv.Status(lockID)
			if err != nil {
				b.Fatalf("Status failed: %v", err)
			}

			// Release
			_, err = srv.Release(ReleaseReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseID:  resp.LeaseID,
			})
			if err != nil {
				b.Fatalf("Release failed: %v", err)
			}
		}
	})
}

// BenchmarkWithQueue tests acquire with queueing enabled
func BenchmarkWithQueue(b *testing.B) {
	srv := setupBenchServer(b)

	// Create a set of locks that will have contention
	numLocks := 10
	locks := make([]string, numLocks)
	for i := 0; i < numLocks; i++ {
		locks[i] = fmt.Sprintf("queue-lock-%d", i)
	}
	// Ensure all queue locks exist before timing.
	b.StopTimer()
	for i := 0; i < numLocks; i++ {
		ensureLockCreated(b, srv, locks[i])
	}
	b.StartTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()
		i := 0

		for pb.Next() {
			lockID := locks[i%numLocks]
			i++

			resp, err := srv.Acquire(AcquireReq{
				LockID:   lockID,
				ClientID: clientID,
				LeaseTTL: Duration(100 * time.Millisecond),
				Wait:     true,
				Timeout:  Duration(1 * time.Second),
			})
			if err != nil {
				b.Fatalf("Acquire failed: %v", err)
			}

			if resp.State == "granted" {
				_, err = srv.Release(ReleaseReq{
					LockID:   lockID,
					ClientID: clientID,
					LeaseID:  resp.LeaseID,
				})
				if err != nil {
					b.Fatalf("Release failed: %v", err)
				}
			} else if resp.State == "queued" {
				// Remove from queue
				_, _ = srv.QueueDel(lockID, resp.WaitID, clientID)
			}
		}
	})
}

// BenchmarkCreate tests lock creation
func BenchmarkCreate(b *testing.B) {
	srv := setupBenchServer(b)

	lockCfg := LockConfig{
		MaxTTL:         Duration(5 * time.Minute),
		DefaultTTL:     Duration(30 * time.Second),
		MaxQueue:       10,
		QueueType:      QueueTypeFIFO,
		DefaultTimeout: Duration(5 * time.Minute),
	}

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		clientID := uuid.New().String()
		lockNum := 0

		for pb.Next() {
			lockID := fmt.Sprintf("create-lock-%s-%d", clientID, lockNum)
			lockNum++

			_, err := srv.Create(lockID, lockCfg, nil, "")
			if err != nil {
				b.Fatalf("Create failed: %v", err)
			}

			// Clean up immediately to avoid accumulating lock goroutines
			if _, err := srv.Delete(lockID); err != nil {
				b.Fatalf("Delete failed: %v", err)
			}
		}
	})
}

// BenchmarkStatus tests status checks
func BenchmarkStatus(b *testing.B) {
	srv := setupBenchServer(b)

	// Pre-create locks
	numLocks := 100
	locks := make([]string, numLocks)
	for i := 0; i < numLocks; i++ {
		locks[i] = fmt.Sprintf("status-lock-%d", i)
		clientID := uuid.New().String()
		ensureLockCreated(b, srv, locks[i])
		_, err := srv.Acquire(AcquireReq{
			LockID:   locks[i],
			ClientID: clientID,
			LeaseTTL: Duration(5 * time.Minute),
		})
		if err != nil {
			b.Fatalf("Setup acquire failed: %v", err)
		}
	}
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lockID := locks[i%numLocks]
			i++

			_, err := srv.Status(lockID)
			if err != nil {
				b.Fatalf("Status failed: %v", err)
			}
		}
	})
}

// BenchmarkHighContentionScenario simulates realistic high-load scenario
func BenchmarkHighContentionScenario(b *testing.B) {
	srv := setupBenchServer(b)

	// Setup: Create some locks upfront
	setupLocks := 50
	for i := 0; i < setupLocks; i++ {
		lockID := fmt.Sprintf("scenario-lock-%d", i)
		clientID := uuid.New().String()
		ensureLockCreated(b, srv, lockID)
		srv.Acquire(AcquireReq{
			LockID:   lockID,
			ClientID: clientID,
			LeaseTTL: Duration(10 * time.Second),
		})
	}
	b.ReportAllocs()

	var wg sync.WaitGroup
	numWorkers := 100
	opsPerWorker := b.N / numWorkers
	if opsPerWorker == 0 {
		opsPerWorker = 1
	}

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			clientID := uuid.New().String()

			for i := 0; i < opsPerWorker; i++ {
				// 50% try existing locks, 50% new locks
				var lockID string
				if i%2 == 0 {
					lockID = fmt.Sprintf("scenario-lock-%d", i%setupLocks)
				} else {
					lockID = fmt.Sprintf("new-lock-%d-%d", workerID, i)
				}

				// Acquire
				resp, err := srv.Acquire(AcquireReq{
					LockID:   lockID,
					ClientID: clientID,
					LeaseTTL: Duration(1 * time.Second),
					Wait:     false,
				})
				if err != nil {
					continue
				}

				if resp.State == "granted" {
					// Do some work (status check)
					srv.Status(lockID)

					// Release
					srv.Release(ReleaseReq{
						LockID:   lockID,
						ClientID: clientID,
						LeaseID:  resp.LeaseID,
					})
				}
			}
		}(w)
	}

	wg.Wait()
}

// BenchmarkThroughput_AcquireRelease measures raw throughput
func BenchmarkThroughput_AcquireRelease(b *testing.B) {
	tests := []struct {
		name     string
		numLocks int
		parallel int
	}{
		{"1Lock", 1, 8},
		{"10Locks", 10, 8},
		{"100Locks", 100, 8},
		{"1000Locks", 1000, 8},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			srv := setupBenchServer(b)

			// Pre-create lock IDs
			lockIDs := make([]string, tt.numLocks)
			for i := 0; i < tt.numLocks; i++ {
				lockIDs[i] = fmt.Sprintf("throughput-lock-%d", i)
			}
			// Ensure all locks exist before timing.
			b.StopTimer()
			for i := 0; i < tt.numLocks; i++ {
				ensureLockCreated(b, srv, lockIDs[i])
			}
			b.StartTimer()
			b.ReportAllocs()

			b.SetParallelism(tt.parallel)
			b.RunParallel(func(pb *testing.PB) {
				clientID := uuid.New().String()
				i := 0

				for pb.Next() {
					lockID := lockIDs[i%tt.numLocks]
					i++

					resp, _ := srv.Acquire(AcquireReq{
						LockID:   lockID,
						ClientID: clientID,
						LeaseTTL: Duration(5 * time.Second),
					})

					if resp.State == "granted" {
						srv.Release(ReleaseReq{
							LockID:   lockID,
							ClientID: clientID,
							LeaseID:  resp.LeaseID,
						})
					}
				}
			})
		})
	}
}
