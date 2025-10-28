package core

import (
	"context"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		cfg := ServerConfig{
			NumShards:           16,
			ExpiryTick:          50 * time.Millisecond,
			DefaultMaxTTL:       5 * time.Minute,
			DefaultTTL:          30 * time.Second,
			DefaultQueue:        10,
			DefaultQueueTimeout: 5 * time.Minute,
		}

		srv, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}

		if len(srv.shards) != 16 {
			t.Errorf("Shards count = %d, want 16", len(srv.shards))
		}
	})

	t.Run("with zero shards uses default", func(t *testing.T) {
		cfg := ServerConfig{
			NumShards: 0,
		}

		srv, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}

		if len(srv.shards) != 32 {
			t.Errorf("Shards count = %d, want 32 (default)", len(srv.shards))
		}
	})

	t.Run("with zero expiry tick uses default", func(t *testing.T) {
		cfg := ServerConfig{
			NumShards:  4,
			ExpiryTick: 0,
		}

		srv, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}

		if srv.cfg.ExpiryTick != 50*time.Millisecond {
			t.Errorf("ExpiryTick = %v, want 50ms (default)", srv.cfg.ExpiryTick)
		}
	})
}

func TestServerStartStop(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	srv.Start()

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	srv.Stop(ctx)

	if ctx.Err() != nil {
		t.Error("Server should stop before context timeout")
	}
}

func TestServerSharding(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           8,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	lockID1 := "test-lock-1"
	lockID2 := "test-lock-2"

	shard1 := srv.shardOf(lockID1)
	shard2 := srv.shardOf(lockID2)

	sameID := srv.shardOf(lockID1)
	if shard1 != sameID {
		t.Error("Same lockID should always route to same shard")
	}

	allSame := true
	for i := 0; i < 100; i++ {
		if srv.shardOf("lock-"+string(rune(i))).id != shard1.id {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("Distribution appears to be broken - all locks go to same shard")
	}

	_ = shard2
}

func TestServerCreate(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	resp, err := srv.Create("test-lock", lockCfg, nil, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !resp.Created {
		t.Error("Lock should be created")
	}

	resp2, err := srv.Create("test-lock", lockCfg, nil, "")
	if err == nil {
		t.Error("Creating duplicate lock should error")
	}
	if resp2.Created {
		t.Error("Duplicate lock should not be created")
	}
}

func TestServerAcquireRelease(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	acquireResp, err := srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(10 * time.Second),
	})

	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	if acquireResp.State != "granted" {
		t.Errorf("State = %s, want granted", acquireResp.State)
	}

	releaseResp, err := srv.Release(ReleaseReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseID:  acquireResp.LeaseID,
	})

	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	if releaseResp.State != "released" {
		t.Errorf("State = %s, want released", releaseResp.State)
	}
}

func TestServerList(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           8,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("lock-1", lockCfg, nil, "")
	srv.Create("lock-2", lockCfg, nil, "")
	srv.Create("lock-3", lockCfg, nil, "")

	locks, err := srv.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(locks) != 3 {
		t.Errorf("List length = %d, want 3", len(locks))
	}
}

func TestServerStatus(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	statusResp, err := srv.Status("test-lock")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if statusResp.Status.Owner != nil {
		t.Error("New lock should have no owner")
	}

	srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
	})

	statusResp2, err := srv.Status("test-lock")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if statusResp2.Status.Owner == nil {
		t.Error("Acquired lock should have owner")
	}
}

func TestServerQueue(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "owner",
	})

	waitResp, err := srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "waiter-1",
		Wait:     true,
	})

	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	if waitResp.State != "queued" {
		t.Errorf("State = %s, want queued", waitResp.State)
	}

	queueResp, err := srv.QueueGet("test-lock", 10)
	if err != nil {
		t.Fatalf("QueueGet() error = %v", err)
	}

	if len(queueResp.Queue) != 1 {
		t.Errorf("Queue length = %d, want 1", len(queueResp.Queue))
	}

	delResp, err := srv.QueueDel("test-lock", waitResp.WaitID, "waiter-1")
	if err != nil {
		t.Fatalf("QueueDel() error = %v", err)
	}

	if !delResp.Removed {
		t.Error("Queue entry should be removed")
	}
}

func TestServerDelete(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	deleteResp, err := srv.Delete("test-lock")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if !deleteResp.Deleted {
		t.Error("Lock should be deleted")
	}

	_, err = srv.Status("test-lock")
	if err == nil {
		t.Error("Status should error for deleted lock")
	}
}

func TestServerRefresh(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	acquireResp, err := srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	refreshResp, err := srv.Refresh(RefreshReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseID:  acquireResp.LeaseID,
		NewTTL:   Duration(20 * time.Second),
	})

	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if refreshResp.State != "granted" {
		t.Errorf("State = %s, want granted", refreshResp.State)
	}
}

func TestServerMetrics(t *testing.T) {
	cfg := ServerConfig{
		NumShards:           4,
		ExpiryTick:          50 * time.Millisecond,
		DefaultMaxTTL:       5 * time.Minute,
		DefaultTTL:          30 * time.Second,
		DefaultQueue:        10,
		DefaultQueueTimeout: 5 * time.Minute,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.Start()
	defer srv.Stop(context.Background())

	lockCfg := LockConfig{
		MaxTTL:     Duration(60 * time.Second),
		DefaultTTL: Duration(10 * time.Second),
		MaxQueue:   5,
		QueueType:  QueueTypeFIFO,
	}

	srv.Create("test-lock", lockCfg, nil, "")

	srv.Acquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
	})

	metricsResp, err := srv.Metrics("test-lock")
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}

	if metricsResp.Metrics.TotalAcquires < 1 {
		t.Error("TotalAcquires should be at least 1")
	}
}
