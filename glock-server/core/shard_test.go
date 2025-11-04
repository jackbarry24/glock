package core

import (
	"sync"
	"testing"
	"time"
)

type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

func testShardConfig() ServerConfig {
	return ServerConfig{
		NumShards:            1,
		ExpiryTick:           50 * time.Millisecond,
		Capacity:             0,
		DefaultMaxTTL:        5 * time.Minute,
		DefaultTTL:           30 * time.Second,
		DefaultQueueCapacity: 10,
		DefaultQueueTimeout:  5 * time.Minute,
	}
}

func TestShardCreate(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	t.Run("create new lock", func(t *testing.T) {
		replyCh := make(chan CreateResp, 1)
		req := CreateReq{
			LockID: "test-lock",
			Config: LockConfig{
				MaxTTL:        Duration(60 * time.Second),
				DefaultTTL:    Duration(10 * time.Second),
				QueueCapacity: 5,
				QueueType:     QueueTypeFIFO,
			},
			Reply: replyCh,
		}

		sh.handleCreate(req)
		resp := <-replyCh

		if !resp.Created {
			t.Error("Lock should be created")
		}
		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if resp.LockID != "test-lock" {
			t.Errorf("LockID = %s, want test-lock", resp.LockID)
		}
	})

	t.Run("create duplicate lock", func(t *testing.T) {
		replyCh := make(chan CreateResp, 1)
		req := CreateReq{
			LockID: "test-lock",
			Config: LockConfig{
				MaxTTL:        Duration(60 * time.Second),
				DefaultTTL:    Duration(10 * time.Second),
				QueueCapacity: 5,
				QueueType:     QueueTypeFIFO,
			},
			Reply: replyCh,
		}

		sh.handleCreate(req)
		resp := <-replyCh

		if resp.Created {
			t.Error("Lock should not be created (duplicate)")
		}
		if resp.Err != ErrLockExists {
			t.Errorf("Expected ErrLockExists, got %v", resp.Err)
		}
	})
}

func TestShardAcquire(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	lockCfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
	}

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: lockCfg,
		Reply:  createCh,
	})
	<-createCh

	t.Run("acquire free lock", func(t *testing.T) {
		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseTTL: Duration(10 * time.Second),
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "granted" {
			t.Errorf("State = %s, want granted", resp.State)
		}
		if resp.LeaseID == "" {
			t.Error("LeaseID should not be empty")
		}
		if resp.OwnerClientID != "client-1" {
			t.Errorf("OwnerClientID = %s, want client-1", resp.OwnerClientID)
		}
	})

	t.Run("acquire busy lock without wait", func(t *testing.T) {
		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-2",
			Wait:     false,
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "busy" {
			t.Errorf("State = %s, want busy", resp.State)
		}
		if resp.Err != ErrLockBusy {
			t.Errorf("Expected ErrLockBusy, got %v", resp.Err)
		}
	})

	t.Run("acquire busy lock with wait - queue", func(t *testing.T) {
		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-2",
			Wait:     true,
			Timeout:  Duration(1 * time.Minute),
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "queued" {
			t.Errorf("State = %s, want queued", resp.State)
		}
		if resp.WaitID == "" {
			t.Error("WaitID should not be empty")
		}
		if resp.Position != 1 {
			t.Errorf("Position = %d, want 1", resp.Position)
		}
	})

	t.Run("queue full", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			replyCh := make(chan AcquireResp, 1)
			req := AcquireReq{
				LockID:   "test-lock",
				ClientID: "client-" + string(rune('3'+i)),
				Wait:     true,
				Reply:    replyCh,
			}
			sh.handleAcquire(req)
			<-replyCh
		}

		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-overflow",
			Wait:     true,
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "busy" {
			t.Errorf("State = %s, want busy (queue full)", resp.State)
		}
		if resp.Err != ErrQueueFull {
			t.Errorf("Expected ErrQueueFull, got %v", resp.Err)
		}
	})

	t.Run("acquire nonexistent lock", func(t *testing.T) {
		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "auto-created-lock",
			ClientID: "client-auto",
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "not_found" {
			t.Errorf("State = %s, want not_found", resp.State)
		}
		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
		if _, exists := sh.locks["auto-created-lock"]; exists {
			t.Error("Lock should not be auto-created")
		}
	})
}

func TestShardRefresh(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(10 * time.Second),
		Reply:    acquireCh,
	})
	acqResp := <-acquireCh

	t.Run("refresh valid lease", func(t *testing.T) {
		replyCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  acqResp.LeaseID,
			NewTTL:   Duration(20 * time.Second),
			Reply:    replyCh,
		}

		sh.handleRefresh(req)
		resp := <-replyCh

		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if resp.State != "granted" {
			t.Errorf("State = %s, want granted", resp.State)
		}
	})

	t.Run("refresh nonexistent lock", func(t *testing.T) {
		replyCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "nonexistent",
			ClientID: "client-1",
			LeaseID:  "fake-lease",
			Reply:    replyCh,
		}

		sh.handleRefresh(req)
		resp := <-replyCh

		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
	})

	t.Run("refresh with wrong client", func(t *testing.T) {
		replyCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "test-lock",
			ClientID: "wrong-client",
			LeaseID:  acqResp.LeaseID,
			Reply:    replyCh,
		}

		sh.handleRefresh(req)
		resp := <-replyCh

		if resp.Err != ErrNotOwner {
			t.Errorf("Expected ErrNotOwner, got %v", resp.Err)
		}
	})

	t.Run("refresh with wrong lease", func(t *testing.T) {
		replyCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  "wrong-lease",
			Reply:    replyCh,
		}

		sh.handleRefresh(req)
		resp := <-replyCh

		if resp.Err != ErrNotOwner {
			t.Errorf("Expected ErrNotOwner, got %v", resp.Err)
		}
	})

	t.Run("refresh enforces max ttl", func(t *testing.T) {
		replyCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  acqResp.LeaseID,
			NewTTL:   Duration(120 * time.Second),
			Reply:    replyCh,
		}

		sh.handleRefresh(req)
		resp := <-replyCh

		if resp.Err != nil {
			t.Fatalf("Unexpected error: %v", resp.Err)
		}

		lock := sh.locks["test-lock"]
		expectedExpiry := clock.Now().Add(60 * time.Second)
		if !lock.owner.ExpiresAt.Equal(expectedExpiry) {
			t.Errorf("Expiry should be capped at max TTL (60s)")
		}
	})
}

func TestShardRelease(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		Reply:    acquireCh,
	})
	acqResp := <-acquireCh

	t.Run("release valid lease", func(t *testing.T) {
		replyCh := make(chan ReleaseResp, 1)
		req := ReleaseReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  acqResp.LeaseID,
			Reply:    replyCh,
		}

		sh.handleRelease(req)
		resp := <-replyCh

		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if resp.State != "released" {
			t.Errorf("State = %s, want released", resp.State)
		}

		lock := sh.locks["test-lock"]
		if lock.owner != nil {
			t.Error("Lock should have no owner after release")
		}
	})

	t.Run("release nonexistent lock", func(t *testing.T) {
		replyCh := make(chan ReleaseResp, 1)
		req := ReleaseReq{
			LockID:   "nonexistent",
			ClientID: "client-1",
			LeaseID:  "fake-lease",
			Reply:    replyCh,
		}

		sh.handleRelease(req)
		resp := <-replyCh

		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
	})

	t.Run("release with wrong client", func(t *testing.T) {
		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-2",
			Reply:    acquireCh,
		})
		acq := <-acquireCh

		replyCh := make(chan ReleaseResp, 1)
		req := ReleaseReq{
			LockID:   "test-lock",
			ClientID: "wrong-client",
			LeaseID:  acq.LeaseID,
			Reply:    replyCh,
		}

		sh.handleRelease(req)
		resp := <-replyCh

		if resp.Err != ErrNotOwner {
			t.Errorf("Expected ErrNotOwner, got %v", resp.Err)
		}
	})
}

func TestShardReleasePromotesQueue(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		Reply:    acquireCh,
	})
	acq := <-acquireCh

	queueCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-2",
		Wait:     true,
		Reply:    queueCh,
	})
	<-queueCh

	releaseCh := make(chan ReleaseResp, 1)
	sh.handleRelease(ReleaseReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseID:  acq.LeaseID,
		Reply:    releaseCh,
	})
	releaseResp := <-releaseCh

	if releaseResp.NextOwnerClientID != "client-2" {
		t.Errorf("NextOwnerClientID = %s, want client-2", releaseResp.NextOwnerClientID)
	}

	lock := sh.locks["test-lock"]
	if lock.owner == nil {
		t.Error("Lock should have a new owner from queue")
	}
	if lock.owner.ClientID != "client-2" {
		t.Errorf("New owner = %s, want client-2", lock.owner.ClientID)
	}
	if len(lock.queue) != 0 {
		t.Errorf("Queue length = %d, want 0", len(lock.queue))
	}
}

func TestShardQueueOperations(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "owner",
		Reply:    acquireCh,
	})
	<-acquireCh

	var waitIDs []string
	for i := 0; i < 3; i++ {
		queueCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-" + string(rune('1'+i)),
			Wait:     true,
			Reply:    queueCh,
		})
		resp := <-queueCh
		waitIDs = append(waitIDs, resp.WaitID)
	}

	t.Run("get queue", func(t *testing.T) {
		replyCh := make(chan QueueGetResp, 1)
		sh.handleQueueGet(QueueGetReq{
			LockID: "test-lock",
			Limit:  10,
			Reply:  replyCh,
		})
		resp := <-replyCh

		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if len(resp.Queue) != 3 {
			t.Errorf("Queue length = %d, want 3", len(resp.Queue))
		}
		for i, entry := range resp.Queue {
			if entry.Position != i+1 {
				t.Errorf("Entry %d position = %d, want %d", i, entry.Position, i+1)
			}
		}
	})

	t.Run("delete from queue", func(t *testing.T) {
		replyCh := make(chan QueueDelResp, 1)
		sh.handleQueueDel(QueueDelReq{
			LockID:   "test-lock",
			WaitID:   waitIDs[1],
			ClientID: "client-2",
			Reply:    replyCh,
		})
		resp := <-replyCh

		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if !resp.Removed {
			t.Error("Entry should be removed")
		}

		lock := sh.locks["test-lock"]
		if len(lock.queue) != 2 {
			t.Errorf("Queue length = %d, want 2", len(lock.queue))
		}
	})

	t.Run("delete nonexistent entry", func(t *testing.T) {
		replyCh := make(chan QueueDelResp, 1)
		sh.handleQueueDel(QueueDelReq{
			LockID:   "test-lock",
			WaitID:   "nonexistent",
			ClientID: "nobody",
			Reply:    replyCh,
		})
		resp := <-replyCh

		if resp.Err != ErrWaitNotFound {
			t.Errorf("Expected ErrWaitNotFound, got %v", resp.Err)
		}
		if resp.Removed {
			t.Error("Entry should not be removed")
		}
	})
}

func TestShardLIFOQueue(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeLIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "owner",
		Reply:    acquireCh,
	})
	acq := <-acquireCh

	for i := 0; i < 3; i++ {
		queueCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-" + string(rune('1'+i)),
			Wait:     true,
			Reply:    queueCh,
		})
		<-queueCh
	}

	releaseCh := make(chan ReleaseResp, 1)
	sh.handleRelease(ReleaseReq{
		LockID:   "test-lock",
		ClientID: "owner",
		LeaseID:  acq.LeaseID,
		Reply:    releaseCh,
	})
	<-releaseCh

	lock := sh.locks["test-lock"]
	if lock.owner.ClientID != "client-3" {
		t.Errorf("LIFO: new owner = %s, want client-3 (last added)", lock.owner.ClientID)
	}
}

func TestShardExpiry(t *testing.T) {
	startTime := time.Now()
	clock := NewFakeClock(startTime)
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseTTL: Duration(5 * time.Second),
		Reply:    acquireCh,
	})
	<-acquireCh

	lock := sh.locks["test-lock"]
	if lock.owner == nil {
		t.Fatal("Lock should have owner")
	}

	clock.Advance(6 * time.Second)
	sh.processExpiries()

	if lock.owner != nil {
		t.Error("Lock owner should be expired and removed")
	}
}

func TestShardQueueTimeout(t *testing.T) {
	startTime := time.Now()
	clock := NewFakeClock(startTime)
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "owner",
		Reply:    acquireCh,
	})
	<-acquireCh

	queueCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "waiter",
		Wait:     true,
		Timeout:  Duration(5 * time.Second),
		Reply:    queueCh,
	})
	<-queueCh

	lock := sh.locks["test-lock"]
	if len(lock.queue) != 1 {
		t.Fatalf("Queue length = %d, want 1", len(lock.queue))
	}

	clock.Advance(6 * time.Second)
	sh.processExpiries()

	if len(lock.queue) != 0 {
		t.Errorf("Queue entry should be expired, got length %d", len(lock.queue))
	}
}

func TestShardDelete(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	t.Run("delete existing lock", func(t *testing.T) {
		replyCh := make(chan DeleteResp, 1)
		sh.handleDelete(DeleteReq{
			LockID: "test-lock",
			Reply:  replyCh,
		})
		resp := <-replyCh

		if !resp.Deleted {
			t.Error("Lock should be deleted")
		}
		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}

		if _, exists := sh.locks["test-lock"]; exists {
			t.Error("Lock should not exist after deletion")
		}
	})

	t.Run("delete nonexistent lock", func(t *testing.T) {
		replyCh := make(chan DeleteResp, 1)
		sh.handleDelete(DeleteReq{
			LockID: "nonexistent",
			Reply:  replyCh,
		})
		resp := <-replyCh

		if resp.Deleted {
			t.Error("Should not report deleted for nonexistent lock")
		}
		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
	})
}

func TestShardStatus(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	t.Run("status of free lock", func(t *testing.T) {
		replyCh := make(chan StatusResp, 1)
		sh.handleStatus(StatusReq{
			LockID: "test-lock",
			Reply:  replyCh,
		})
		resp := <-replyCh

		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
		if resp.Status.Owner != nil {
			t.Error("Free lock should have no owner")
		}
		if resp.Status.QueueLen != 0 {
			t.Errorf("Queue length = %d, want 0", resp.Status.QueueLen)
		}
	})

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		Reply:    acquireCh,
	})
	<-acquireCh

	t.Run("status of acquired lock", func(t *testing.T) {
		replyCh := make(chan StatusResp, 1)
		sh.handleStatus(StatusReq{
			LockID: "test-lock",
			Reply:  replyCh,
		})
		resp := <-replyCh

		if resp.Status.Owner == nil {
			t.Error("Acquired lock should have owner")
		}
		if resp.Status.Owner.ClientID != "client-1" {
			t.Errorf("Owner = %s, want client-1", resp.Status.Owner.ClientID)
		}
	})

	t.Run("status of nonexistent lock", func(t *testing.T) {
		replyCh := make(chan StatusResp, 1)
		sh.handleStatus(StatusReq{
			LockID: "nonexistent",
			Reply:  replyCh,
		})
		resp := <-replyCh

		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
	})
}

func TestShardMetrics(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 2,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	acquireCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		Reply:    acquireCh,
	})
	acq := <-acquireCh

	for i := 0; i < 2; i++ {
		qCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-" + string(rune('2'+i)),
			Wait:     true,
			Reply:    qCh,
		})
		<-qCh
	}

	failCh := make(chan AcquireResp, 1)
	sh.handleAcquire(AcquireReq{
		LockID:   "test-lock",
		ClientID: "client-fail",
		Wait:     true,
		Reply:    failCh,
	})
	<-failCh

	releaseCh := make(chan ReleaseResp, 1)
	sh.handleRelease(ReleaseReq{
		LockID:   "test-lock",
		ClientID: "client-1",
		LeaseID:  acq.LeaseID,
		Reply:    releaseCh,
	})
	<-releaseCh

	metricsCh := make(chan MetricsResp, 1)
	sh.handleMetrics(MetricsReq{
		LockID: "test-lock",
		Reply:  metricsCh,
	})
	metrics := <-metricsCh

	if metrics.Err != nil {
		t.Fatalf("Unexpected error: %v", metrics.Err)
	}

	m := metrics.Metrics
	if m.TotalAcquires != 4 {
		t.Errorf("TotalAcquires = %d, want 4", m.TotalAcquires)
	}
	if m.SuccessAcquires != 1 {
		t.Errorf("SuccessAcquires = %d, want 1", m.SuccessAcquires)
	}
	if m.FailedAcquires != 1 {
		t.Errorf("FailedAcquires = %d, want 1", m.FailedAcquires)
	}
	if m.TotalQueued != 2 {
		t.Errorf("TotalQueued = %d, want 2", m.TotalQueued)
	}
	if m.TotalReleases != 1 {
		t.Errorf("TotalReleases = %d, want 1", m.TotalReleases)
	}
	if m.OwnerChanges != 2 {
		t.Errorf("OwnerChanges = %d, want 2 (initial + promotion)", m.OwnerChanges)
	}
}

func TestShardList(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "lock-1",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh,
	})
	<-createCh

	createCh2 := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "lock-2",
		Config: LockConfig{
			MaxTTL:        Duration(60 * time.Second),
			DefaultTTL:    Duration(10 * time.Second),
			QueueCapacity: 5,
			QueueType:     QueueTypeFIFO,
		},
		Reply: createCh2,
	})
	<-createCh2

	replyCh := make(chan []string, 1)
	sh.handleList(listReq{Reply: replyCh})
	locks := <-replyCh

	if len(locks) != 2 {
		t.Errorf("List length = %d, want 2", len(locks))
	}

	hasLock1 := false
	hasLock2 := false
	for _, lock := range locks {
		if lock == "lock-1" {
			hasLock1 = true
		}
		if lock == "lock-2" {
			hasLock2 = true
		}
	}

	if !hasLock1 || !hasLock2 {
		t.Errorf("Missing locks in list: %v", locks)
	}
}

func TestShardCapacityEnforcement(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	cfg.Capacity = 2
	sh := newShard(0, cfg, clock)

	lockCfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	t.Run("create up to capacity", func(t *testing.T) {
		createCh1 := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: "lock-1",
			Config: lockCfg,
			Reply:  createCh1,
		})
		resp1 := <-createCh1

		if !resp1.Created {
			t.Error("First lock should be created")
		}
		if resp1.Err != nil {
			t.Errorf("Unexpected error: %v", resp1.Err)
		}

		createCh2 := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: "lock-2",
			Config: lockCfg,
			Reply:  createCh2,
		})
		resp2 := <-createCh2

		if !resp2.Created {
			t.Error("Second lock should be created")
		}
		if resp2.Err != nil {
			t.Errorf("Unexpected error: %v", resp2.Err)
		}
	})

	t.Run("create exceeds capacity", func(t *testing.T) {
		createCh3 := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: "lock-3",
			Config: lockCfg,
			Reply:  createCh3,
		})
		resp3 := <-createCh3

		if resp3.Created {
			t.Error("Third lock should not be created (capacity reached)")
		}
		if resp3.Err != ErrCapacityReached {
			t.Errorf("Expected ErrCapacityReached, got %v", resp3.Err)
		}
	})

	// acquire on a nonexistent lock should return not found, irrespective of capacity
	t.Run("acquire nonexistent returns not_found at capacity", func(t *testing.T) {
		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "lock-4",
			ClientID: "client-1",
			Reply:    acquireCh,
		})
		resp := <-acquireCh

		if resp.State != "not_found" {
			t.Errorf("State = %s, want not_found", resp.State)
		}
		if resp.Err != ErrLockNotFound {
			t.Errorf("Expected ErrLockNotFound, got %v", resp.Err)
		}
	})

	t.Run("capacity freed after delete", func(t *testing.T) {
		deleteCh := make(chan DeleteResp, 1)
		sh.handleDelete(DeleteReq{
			LockID: "lock-1",
			Reply:  deleteCh,
		})
		<-deleteCh

		createCh := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: "lock-5",
			Config: lockCfg,
			Reply:  createCh,
		})
		resp := <-createCh

		if !resp.Created {
			t.Error("Lock should be created after capacity freed")
		}
		if resp.Err != nil {
			t.Errorf("Unexpected error: %v", resp.Err)
		}
	})

	t.Run("unlimited capacity", func(t *testing.T) {
		cfgUnlimited := testShardConfig()
		cfgUnlimited.Capacity = 0
		shUnlimited := newShard(0, cfgUnlimited, clock)

		for i := 0; i < 100; i++ {
			createCh := make(chan CreateResp, 1)
			shUnlimited.handleCreate(CreateReq{
				LockID: "unlimited-lock-" + string(rune(i)),
				Config: lockCfg,
				Reply:  createCh,
			})
			resp := <-createCh

			if !resp.Created && resp.Err == ErrCapacityReached {
				t.Errorf("Unlimited capacity (0) should allow any number of locks, failed at lock %d", i)
				break
			}
		}
	})
}

func TestShardFencingTokens(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := testShardConfig()
	sh := newShard(0, cfg, clock)

	lockCfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 5,
		QueueType:     QueueTypeFIFO,
	}

	createCh := make(chan CreateResp, 1)
	sh.handleCreate(CreateReq{
		LockID: "test-lock",
		Config: lockCfg,
		Reply:  createCh,
	})
	<-createCh

	t.Run("first acquire gets fence token 1", func(t *testing.T) {
		replyCh := make(chan AcquireResp, 1)
		req := AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			Reply:    replyCh,
		}

		sh.handleAcquire(req)
		resp := <-replyCh

		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}
		if resp.FenceToken != 1 {
			t.Errorf("FenceToken = %d, want 1", resp.FenceToken)
		}
	})

	t.Run("refresh returns same fence token", func(t *testing.T) {
		lock := sh.locks["test-lock"]
		if lock == nil || lock.owner == nil {
			t.Fatal("Lock or owner is nil")
		}

		refreshCh := make(chan RefreshResp, 1)
		req := RefreshReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  lock.owner.LeaseID,
			NewTTL:   Duration(10 * time.Second),
			Reply:    refreshCh,
		}

		sh.handleRefresh(req)
		resp := <-refreshCh

		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}
		if resp.FenceToken != 1 {
			t.Errorf("FenceToken = %d, want 1 (should not change on refresh)", resp.FenceToken)
		}
	})

	t.Run("release and reacquire increments fence token", func(t *testing.T) {
		lock := sh.locks["test-lock"]
		if lock == nil || lock.owner == nil {
			t.Fatal("Lock or owner is nil")
		}

		releaseCh := make(chan ReleaseResp, 1)
		sh.handleRelease(ReleaseReq{
			LockID:   "test-lock",
			ClientID: "client-1",
			LeaseID:  lock.owner.LeaseID,
			Reply:    releaseCh,
		})
		<-releaseCh

		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-2",
			Reply:    acquireCh,
		})
		resp := <-acquireCh

		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}
		if resp.FenceToken != 2 {
			t.Errorf("FenceToken = %d, want 2 (should increment on new owner)", resp.FenceToken)
		}
	})

	t.Run("queue promotion increments fence token", func(t *testing.T) {
		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   "test-lock",
			ClientID: "client-3",
			Wait:     true,
			Timeout:  Duration(1 * time.Minute),
			Reply:    acquireCh,
		})
		resp := <-acquireCh

		if resp.State != "queued" {
			t.Fatalf("State = %s, want queued", resp.State)
		}

		lock := sh.locks["test-lock"]
		if lock == nil || lock.owner == nil {
			t.Fatal("Lock or owner is nil")
		}
		oldLeaseID := lock.owner.LeaseID

		releaseCh := make(chan ReleaseResp, 1)
		sh.handleRelease(ReleaseReq{
			LockID:   "test-lock",
			ClientID: "client-2",
			LeaseID:  oldLeaseID,
			Reply:    releaseCh,
		})
		<-releaseCh

		if lock.owner == nil {
			t.Fatal("Owner should be promoted from queue")
		}
		if lock.owner.ClientID != "client-3" {
			t.Errorf("Owner ClientID = %s, want client-3", lock.owner.ClientID)
		}
		if lock.owner.FenceToken != 3 {
			t.Errorf("FenceToken = %d, want 3 (should increment on queue promotion)", lock.owner.FenceToken)
		}
	})

	t.Run("fence tokens are monotonically increasing", func(t *testing.T) {
		testLockID := "monotonic-lock"
		createCh := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: testLockID,
			Config: lockCfg,
			Reply:  createCh,
		})
		<-createCh

		var lastFenceToken int64
		for i := 0; i < 10; i++ {
			acquireCh := make(chan AcquireResp, 1)
			clientID := "client-" + string(rune('a'+i))
			sh.handleAcquire(AcquireReq{
				LockID:   testLockID,
				ClientID: clientID,
				Reply:    acquireCh,
			})
			acquireResp := <-acquireCh

			if acquireResp.State != "granted" {
				t.Fatalf("Acquire %d: State = %s, want granted", i, acquireResp.State)
			}

			if acquireResp.FenceToken <= lastFenceToken {
				t.Errorf("Acquire %d: FenceToken = %d, want > %d (monotonically increasing)", i, acquireResp.FenceToken, lastFenceToken)
			}
			lastFenceToken = acquireResp.FenceToken

			lock := sh.locks[testLockID]
			if lock == nil || lock.owner == nil {
				t.Fatalf("Acquire %d: Lock or owner is nil", i)
			}

			releaseCh := make(chan ReleaseResp, 1)
			sh.handleRelease(ReleaseReq{
				LockID:   testLockID,
				ClientID: clientID,
				LeaseID:  lock.owner.LeaseID,
				Reply:    releaseCh,
			})
			<-releaseCh
		}
	})

	t.Run("fence token in lease struct matches response", func(t *testing.T) {
		testLockID := "lease-match-lock"
		createCh := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: testLockID,
			Config: lockCfg,
			Reply:  createCh,
		})
		<-createCh

		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   testLockID,
			ClientID: "client-test",
			Reply:    acquireCh,
		})
		resp := <-acquireCh

		if resp.State != "granted" {
			t.Fatalf("State = %s, want granted", resp.State)
		}

		lock := sh.locks[testLockID]
		if lock == nil || lock.owner == nil {
			t.Fatal("Lock or owner is nil")
		}

		if lock.owner.FenceToken != resp.FenceToken {
			t.Errorf("Lease.FenceToken = %d, Response.FenceToken = %d, want equal", lock.owner.FenceToken, resp.FenceToken)
		}
	})

	t.Run("expiry and promotion maintains fence token increment", func(t *testing.T) {
		testLockID := "expiry-fence-lock"
		createCh := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: testLockID,
			Config: lockCfg,
			Reply:  createCh,
		})
		<-createCh

		acquireCh1 := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   testLockID,
			ClientID: "client-1",
			LeaseTTL: Duration(5 * time.Second),
			Reply:    acquireCh1,
		})
		resp1 := <-acquireCh1

		if resp1.State != "granted" {
			t.Fatalf("State = %s, want granted", resp1.State)
		}
		firstToken := resp1.FenceToken

		acquireCh2 := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   testLockID,
			ClientID: "client-2",
			Wait:     true,
			Timeout:  Duration(1 * time.Minute),
			Reply:    acquireCh2,
		})
		resp2 := <-acquireCh2

		if resp2.State != "queued" {
			t.Fatalf("State = %s, want queued", resp2.State)
		}

		clock.Advance(6 * time.Second)
		sh.processExpiries()

		lock := sh.locks[testLockID]
		if lock == nil || lock.owner == nil {
			t.Fatal("Lock should have owner after promotion from queue")
		}

		if lock.owner.ClientID != "client-2" {
			t.Errorf("Owner ClientID = %s, want client-2", lock.owner.ClientID)
		}
		if lock.owner.FenceToken != firstToken+1 {
			t.Errorf("FenceToken = %d, want %d (should increment after expiry promotion)", lock.owner.FenceToken, firstToken+1)
		}
	})

	t.Run("LIFO queue promotion increments fence token correctly", func(t *testing.T) {
		testLockID := "lifo-fence-lock"
		lifoConfig := lockCfg
		lifoConfig.QueueType = QueueTypeLIFO

		createCh := make(chan CreateResp, 1)
		sh.handleCreate(CreateReq{
			LockID: testLockID,
			Config: lifoConfig,
			Reply:  createCh,
		})
		<-createCh

		acquireCh := make(chan AcquireResp, 1)
		sh.handleAcquire(AcquireReq{
			LockID:   testLockID,
			ClientID: "client-1",
			Reply:    acquireCh,
		})
		resp1 := <-acquireCh

		if resp1.State != "granted" || resp1.FenceToken != 1 {
			t.Fatalf("First acquire: State = %s, FenceToken = %d, want granted/1", resp1.State, resp1.FenceToken)
		}

		for i := 2; i <= 4; i++ {
			queueCh := make(chan AcquireResp, 1)
			sh.handleAcquire(AcquireReq{
				LockID:   testLockID,
				ClientID: "client-" + string(rune('0'+i)),
				Wait:     true,
				Reply:    queueCh,
			})
			<-queueCh
		}

		lock := sh.locks[testLockID]
		releaseCh := make(chan ReleaseResp, 1)
		sh.handleRelease(ReleaseReq{
			LockID:   testLockID,
			ClientID: "client-1",
			LeaseID:  lock.owner.LeaseID,
			Reply:    releaseCh,
		})
		<-releaseCh

		if lock.owner == nil {
			t.Fatal("Owner should be promoted from LIFO queue")
		}
		if lock.owner.ClientID != "client-4" {
			t.Errorf("LIFO: Owner ClientID = %s, want client-4 (last in queue)", lock.owner.ClientID)
		}
		if lock.owner.FenceToken != 2 {
			t.Errorf("LIFO: FenceToken = %d, want 2", lock.owner.FenceToken)
		}
	})
}
