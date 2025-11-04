package core

import (
	"testing"
	"time"
)

func makeLockForTest(t *testing.T, id string, clock Clock) *Lock {
	cfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	return newLock(id, cfg, nil, clock)
}

func TestLockAcquireBasics(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-1", clock)

	// Default TTL used when LeaseTTL is zero
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-1", ClientID: "c1", LeaseTTL: 0, Reply: areply})
	a := <-areply
	if a.State != "granted" {
		t.Fatalf("state=%s want granted", a.State)
	}
	if l.owner == nil {
		t.Fatalf("owner should be set")
	}
	if !l.owner.ExpiresAt.Equal(clock.Now().Add(10 * time.Second)) {
		t.Fatalf("expiry=%v want %v", l.owner.ExpiresAt, clock.Now().Add(10*time.Second))
	}

	// Clamp to MaxTTL when requested TTL > MaxTTL
	areply2 := make(chan AcquireResp, 1)
	l.handleRelease(ReleaseReq{LockID: "lock-1", ClientID: "c1", LeaseID: a.LeaseID, Reply: make(chan ReleaseResp, 1)})
	l.handleAcquire(AcquireReq{LockID: "lock-1", ClientID: "c2", LeaseTTL: Duration(2 * time.Minute), Reply: areply2})
	a2 := <-areply2
	if a2.State != "granted" {
		t.Fatalf("state=%s want granted", a2.State)
	}
	if !l.owner.ExpiresAt.Equal(clock.Now().Add(60 * time.Second)) {
		t.Fatalf("expiry=%v want %v (max ttl)", l.owner.ExpiresAt, clock.Now().Add(60*time.Second))
	}
}

func TestLockAcquireBusyAndQueue(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-2", clock)

	// Take ownership
	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-2", ClientID: "owner", LeaseTTL: Duration(10 * time.Second), Reply: a1})
	<-a1

	// Busy without wait
	a2 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-2", ClientID: "c2", Wait: false, Reply: a2})
	r2 := <-a2
	if r2.State != "busy" || r2.Err != ErrLockBusy {
		t.Fatalf("busy without wait: state=%s err=%v", r2.State, r2.Err)
	}

	// Queue with default timeout (request Timeout=0 uses lock default)
	a3 := make(chan AcquireResp, 1)
	start := clock.Now()
	l.handleAcquire(AcquireReq{LockID: "lock-2", ClientID: "c3", Wait: true, Timeout: 0, Reply: a3})
	r3 := <-a3
	if r3.State != "queued" || r3.WaitID == "" || len(l.queue) != 1 {
		t.Fatalf("queue: %+v queueLen=%d", r3, len(l.queue))
	}
	if !l.queue[0].timeoutAt.Equal(start.Add(30 * time.Second)) {
		t.Fatalf("queue timeoutAt=%v want %v", l.queue[0].timeoutAt, start.Add(30*time.Second))
	}
}

func TestLockPromotionOnExpiryTick(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-3", clock)

	// Acquire with short TTL
	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-3", ClientID: "owner", LeaseTTL: Duration(5 * time.Second), Reply: a1})
	<-a1

	// Queue a waiter
	a2 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-3", ClientID: "waiter", Wait: true, Timeout: Duration(1 * time.Minute), Reply: a2})
	r2 := <-a2
	if r2.State != "queued" {
		t.Fatalf("enqueue state=%s", r2.State)
	}

	// Advance past expiry and tick
	clock.Advance(6 * time.Second)
	l.processTick()

	if l.owner == nil || l.owner.ClientID != "waiter" {
		t.Fatalf("owner after expiry promotion = %+v want waiter", l.owner)
	}
}

func TestLockReleasePromotesQueue(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-4", clock)

	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-4", ClientID: "owner", Reply: a1})
	r1 := <-a1

	a2 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-4", ClientID: "next", Wait: true, Timeout: Duration(1 * time.Minute), Reply: a2})
	<-a2

	rreply := make(chan ReleaseResp, 1)
	l.handleRelease(ReleaseReq{LockID: "lock-4", ClientID: "owner", LeaseID: r1.LeaseID, Reply: rreply})
	rr := <-rreply
	if rr.Err != nil || rr.NextOwnerClientID != "next" {
		t.Fatalf("release resp: %+v", rr)
	}
	if l.owner == nil || l.owner.ClientID != "next" {
		t.Fatalf("owner after release promotion = %+v", l.owner)
	}
}

func TestLockQueueDelAndGet(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-5", clock)

	// Occupy owner
	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-5", ClientID: "owner", Reply: a1})
	<-a1
	// Add two queue entries
	a2 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-5", ClientID: "c2", Wait: true, Timeout: Duration(1 * time.Minute), Reply: a2})
	q1 := <-a2
	a3 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-5", ClientID: "c3", Wait: true, Timeout: Duration(1 * time.Minute), Reply: a3})
	<-a3

	// Get queue limited to 1
	greply := make(chan QueueGetResp, 1)
	l.handleQueueGet(QueueGetReq{LockID: "lock-5", Limit: 1, Reply: greply})
	g := <-greply
	if len(g.Queue) != 1 || g.Queue[0].ClientID != "c2" || g.Queue[0].Position != 1 {
		t.Fatalf("queue get: %+v", g.Queue)
	}

	// Delete the first queued entry
	dreply := make(chan QueueDelResp, 1)
	l.handleQueueDel(QueueDelReq{LockID: "lock-5", WaitID: q1.WaitID, ClientID: "c2", Reply: dreply})
	d := <-dreply
	if !d.Removed || d.Err != nil {
		t.Fatalf("queue del: %+v", d)
	}
	if len(l.queue) != 1 || l.queue[0].clientID != "c3" {
		t.Fatalf("queue after del len=%d first=%v", len(l.queue), l.queue[0].clientID)
	}
}

func TestLockRefreshAndErrors(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-6", clock)

	// Refresh on empty lock -> invalid lease
	rreply := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-6", ClientID: "c1", LeaseID: "none", Reply: rreply})
	r := <-rreply
	if r.Err != ErrInvalidLease {
		t.Fatalf("expected ErrInvalidLease, got %v", r.Err)
	}

	// Acquire, then refresh as wrong client -> not owner
	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-6", ClientID: "owner", LeaseTTL: Duration(10 * time.Second), Reply: a1})
	a := <-a1

	rreply2 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-6", ClientID: "other", LeaseID: a.LeaseID, NewTTL: Duration(20 * time.Second), Reply: rreply2})
	r2 := <-rreply2
	if r2.Err != ErrNotOwner {
		t.Fatalf("expected ErrNotOwner, got %v", r2.Err)
	}

	// Valid refresh caps to MaxTTL
	rreply3 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-6", ClientID: "owner", LeaseID: a.LeaseID, NewTTL: Duration(2 * time.Minute), Reply: rreply3})
	r3 := <-rreply3
	if r3.Err != nil {
		t.Fatalf("refresh err: %v", r3.Err)
	}
	if !l.owner.ExpiresAt.Equal(clock.Now().Add(60 * time.Second)) {
		t.Fatalf("expiry=%v want %v (max)", l.owner.ExpiresAt, clock.Now().Add(60*time.Second))
	}
}

func TestLockStatusAndMetrics(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-7", clock)

	// Acquire then metrics
	a1 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-7", ClientID: "owner", LeaseTTL: Duration(10 * time.Second), Reply: a1})
	<-a1

	mreply := make(chan MetricsResp, 1)
	l.handleMetrics(MetricsReq{LockID: "lock-7", Reply: mreply})
	m := <-mreply
	if m.Err != nil || m.Metrics.TotalAcquires == 0 || m.Metrics.SuccessAcquires != 1 {
		t.Fatalf("metrics: %+v", m)
	}

	sreply := make(chan StatusResp, 1)
	l.handleStatus(StatusReq{LockID: "lock-7", Reply: sreply})
	s := <-sreply
	if s.Err != nil || s.Status.Owner == nil || s.Status.LockID != "lock-7" {
		t.Fatalf("status: %+v", s)
	}
}

func TestLockIdempotentAcquire(t *testing.T) {
	clock := NewFakeClock(time.Now())
	l := makeLockForTest(t, "lock-8", clock)

	idem := "idem-1"
	r1ch := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-8", ClientID: "c1", LeaseTTL: Duration(10 * time.Second), IdemKey: idem, Reply: r1ch})
	r1 := <-r1ch
	if r1.State != "granted" || r1.LeaseID == "" {
		t.Fatalf("first acquire: %+v", r1)
	}

	r2ch := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-8", ClientID: "c1", LeaseTTL: Duration(10 * time.Second), IdemKey: idem, Reply: r2ch})
	r2 := <-r2ch
	if r2.LeaseID != r1.LeaseID || r2.FenceToken != r1.FenceToken || r2.State != r1.State {
		t.Fatalf("idempotent mismatch: r1=%+v r2=%+v", r1, r2)
	}
}

func TestMaxTTLAbsoluteLifetimeCap(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	l := newLock("lock-maxttl", cfg, nil, clock)

	acquiredAt := clock.Now()

	// Acquire lock
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-maxttl", ClientID: "c1", LeaseTTL: Duration(20 * time.Second), Reply: areply})
	a := <-areply
	if a.State != "granted" {
		t.Fatalf("acquire state=%s want granted", a.State)
	}
	expectedExpiry := acquiredAt.Add(20 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("initial expiry=%v want %v", l.owner.ExpiresAt, expectedExpiry)
	}

	// Refresh 1: should extend, but not beyond absolute cap
	clock.Advance(10 * time.Second)
	rreply1 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-maxttl", ClientID: "c1", LeaseID: a.LeaseID, NewTTL: Duration(50 * time.Second), Reply: rreply1})
	r1 := <-rreply1
	if r1.Err != nil {
		t.Fatalf("refresh err: %v", r1.Err)
	}
	// Should be capped at acquiredAt + MaxTTL (60s), not now + 50s
	expectedCap := acquiredAt.Add(60 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedCap) {
		t.Fatalf("refresh1 expiry=%v want %v (absolute cap)", l.owner.ExpiresAt, expectedCap)
	}

	// Refresh 2: already at cap, should stay at cap
	clock.Advance(5 * time.Second)
	rreply2 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-maxttl", ClientID: "c1", LeaseID: a.LeaseID, NewTTL: Duration(30 * time.Second), Reply: rreply2})
	r2 := <-rreply2
	if r2.Err != nil {
		t.Fatalf("refresh2 err: %v", r2.Err)
	}
	// Should still be at absolute cap
	if !l.owner.ExpiresAt.Equal(expectedCap) {
		t.Fatalf("refresh2 expiry=%v want %v (still at cap)", l.owner.ExpiresAt, expectedCap)
	}

	// Advance past absolute cap and tick - lock should expire
	clock.Advance(50 * time.Second)
	l.processTick()
	if l.owner != nil {
		t.Fatalf("lock should expire after absolute MaxTTL, owner=%+v", l.owner)
	}
}

func TestMaxTTLZeroInfinite(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := LockConfig{
		MaxTTL:        Duration(0),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	l := newLock("lock-infinite", cfg, nil, clock)

	// Acquire lock
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-infinite", ClientID: "c1", LeaseTTL: Duration(20 * time.Second), Reply: areply})
	a := <-areply
	if a.State != "granted" {
		t.Fatalf("acquire state=%s want granted", a.State)
	}

	// Refresh with large TTL - should not be capped
	clock.Advance(10 * time.Second)
	rreply1 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-infinite", ClientID: "c1", LeaseID: a.LeaseID, NewTTL: Duration(100 * time.Second), Reply: rreply1})
	r1 := <-rreply1
	if r1.Err != nil {
		t.Fatalf("refresh err: %v", r1.Err)
	}
	// With MaxTTL=0, should allow large TTL
	expectedExpiry := clock.Now().Add(100 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("refresh expiry=%v want %v (no cap with MaxTTL=0)", l.owner.ExpiresAt, expectedExpiry)
	}

	// Another refresh - should still work without absolute cap
	clock.Advance(50 * time.Second)
	rreply2 := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-infinite", ClientID: "c1", LeaseID: a.LeaseID, NewTTL: Duration(200 * time.Second), Reply: rreply2})
	r2 := <-rreply2
	if r2.Err != nil {
		t.Fatalf("refresh2 err: %v", r2.Err)
	}
	// Should allow unlimited refresh
	expectedExpiry2 := clock.Now().Add(200 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedExpiry2) {
		t.Fatalf("refresh2 expiry=%v want %v (unlimited with MaxTTL=0)", l.owner.ExpiresAt, expectedExpiry2)
	}
}

func TestMaxTTLAcquireInitialCap(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	l := newLock("lock-acq-cap", cfg, nil, clock)

	acquiredAt := clock.Now()

	// Acquire with TTL > MaxTTL - should be capped at now + MaxTTL
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-acq-cap", ClientID: "c1", LeaseTTL: Duration(120 * time.Second), Reply: areply})
	a := <-areply
	if a.State != "granted" {
		t.Fatalf("acquire state=%s want granted", a.State)
	}
	// Should be capped at acquiredAt + MaxTTL (60s), not 120s
	expectedExpiry := acquiredAt.Add(60 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("acquire expiry=%v want %v (capped at MaxTTL)", l.owner.ExpiresAt, expectedExpiry)
	}
}

func TestMaxTTLPromoteRespectsMaxTTL(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(30 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	l := newLock("lock-promote", cfg, nil, clock)

	// Acquire lock
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-promote", ClientID: "c1", LeaseTTL: Duration(20 * time.Second), Reply: areply})
	a := <-areply

	// Queue a waiter
	areply2 := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-promote", ClientID: "c2", Wait: true, Timeout: Duration(1 * time.Minute), Reply: areply2})
	<-areply2

	// Release lock - should promote from queue
	rreply := make(chan ReleaseResp, 1)
	l.handleRelease(ReleaseReq{LockID: "lock-promote", ClientID: "c1", LeaseID: a.LeaseID, Reply: rreply})
	<-rreply

	// Promoted lock should use DefaultTTL (30s), capped at MaxTTL (60s) if needed
	now := clock.Now()
	expectedExpiry := now.Add(30 * time.Second)
	if l.owner == nil || l.owner.ClientID != "c2" {
		t.Fatalf("owner after promote=%+v want c2", l.owner)
	}
	// DefaultTTL is 30s, which is less than MaxTTL (60s), so should use DefaultTTL
	if !l.owner.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("promoted expiry=%v want %v (DefaultTTL)", l.owner.ExpiresAt, expectedExpiry)
	}
}

func TestMaxTTLRefreshExceedsAbsoluteCap(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cfg := LockConfig{
		MaxTTL:        Duration(60 * time.Second),
		DefaultTTL:    Duration(10 * time.Second),
		QueueCapacity: 3,
		QueueType:     QueueTypeFIFO,
		QueueTimeout:  Duration(30 * time.Second),
	}
	l := newLock("lock-cap-exceed", cfg, nil, clock)

	acquiredAt := clock.Now()

	// Acquire lock
	areply := make(chan AcquireResp, 1)
	l.handleAcquire(AcquireReq{LockID: "lock-cap-exceed", ClientID: "c1", LeaseTTL: Duration(20 * time.Second), Reply: areply})
	a := <-areply

	// Advance time so we're close to the absolute cap
	clock.Advance(50 * time.Second)

	// Try to refresh with large TTL - should be capped at absolute limit
	rreply := make(chan RefreshResp, 1)
	l.handleRefresh(RefreshReq{LockID: "lock-cap-exceed", ClientID: "c1", LeaseID: a.LeaseID, NewTTL: Duration(100 * time.Second), Reply: rreply})
	r := <-rreply
	if r.Err != nil {
		t.Fatalf("refresh err: %v", r.Err)
	}
	// Should be capped at acquiredAt + MaxTTL, not now + 100s
	expectedCap := acquiredAt.Add(60 * time.Second)
	if !l.owner.ExpiresAt.Equal(expectedCap) {
		t.Fatalf("refresh expiry=%v want %v (absolute cap)", l.owner.ExpiresAt, expectedCap)
	}

	// Advance past the absolute cap
	clock.Advance(15 * time.Second)
	l.processTick()
	// Lock should expire
	if l.owner != nil {
		t.Fatalf("lock should expire after absolute MaxTTL, owner=%+v", l.owner)
	}
}
