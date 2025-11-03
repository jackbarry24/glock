package core

import "time"

// Lock is an actor responsible for a single lock's lifecycle.
type Lock struct {
	id         string
	cfg        LockConfig
	owner      *Lease
	queue      []queueEntry
	createdAt  time.Time
	metrics    *LockMetrics
	tags       []string
	fenceToken int64

	clock    Clock
	msgCh    chan Msg
	idemKeys map[string]*idemEntry
}

type queueEntry struct {
	waitID    string
	clientID  string
	timeoutAt time.Time
	addedAt   time.Time
}

// internal messages to drive time and shutdown
type lockTick struct{ Reply chan struct{} }

func (lockTick) isMsg() {}

type lockStop struct{ Reply chan struct{} }

func (lockStop) isMsg() {}

func newLock(id string, cfg LockConfig, tags []string, clock Clock) *Lock {
	now := clock.Now()
	if len(tags) > 16 {
		tags = tags[:16]
	}
	return &Lock{
		id:        id,
		cfg:       cfg,
		queue:     make([]queueEntry, 0),
		createdAt: now,
		metrics: &LockMetrics{
			CreatedAt:      now,
			LastActivityAt: now,
			uniqueOwnerIDs: make(map[string]bool),
		},
		tags:     tags,
		clock:    clock,
		msgCh:    make(chan Msg, 256),
		idemKeys: make(map[string]*idemEntry),
	}
}

func (l *Lock) run() {
	for {
		msg := <-l.msgCh
		switch m := msg.(type) {
		case AcquireReq:
			l.handleAcquire(m)
		case RefreshReq:
			l.handleRefresh(m)
		case ReleaseReq:
			l.handleRelease(m)
		case StatusReq:
			l.handleStatus(m)
		case QueueGetReq:
			l.handleQueueGet(m)
		case QueueDelReq:
			l.handleQueueDel(m)
		case MetricsReq:
			l.handleMetrics(m)
		case lockTick:
			l.processTick()
			if m.Reply != nil {
				close(m.Reply)
			}
		case lockStop:
			close(m.Reply)
			return
		}
	}
}

func (l *Lock) processTick() {
	now := l.clock.Now()

	if l.owner != nil && l.owner.ExpiresAt.Before(now) {
		holdDuration := now.Sub(l.owner.AcquiredAt)
		holdNs := holdDuration.Nanoseconds()

		l.metrics.TotalHoldTimeNs += holdNs
		l.metrics.totalHoldCount++
		if l.metrics.totalHoldCount > 0 {
			l.metrics.AvgHoldTimeNs = l.metrics.TotalHoldTimeNs / l.metrics.totalHoldCount
		}
		if holdNs > l.metrics.MaxHoldTimeNs {
			l.metrics.MaxHoldTimeNs = holdNs
		}
		l.metrics.TTLExpirations++

		l.owner = nil
		l.promoteFromQueue()
	}

	for i := len(l.queue) - 1; i >= 0; i-- {
		if l.queue[i].timeoutAt.Before(now) {
			l.metrics.QueueTimeouts++
			l.queue = append(l.queue[:i], l.queue[i+1:]...)
		}
	}

	for key, entry := range l.idemKeys {
		if entry.expiresAt.Before(now) {
			delete(l.idemKeys, key)
		}
	}
}

func (l *Lock) handleAcquire(req AcquireReq) {
	if req.IdemKey != "" {
		if cached, ok := l.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(l.clock.Now()) {
			req.Reply <- cached.response.(AcquireResp)
			return
		}
	}

	resp := AcquireResp{LockID: req.LockID}

	l.metrics.TotalAcquires++
	l.metrics.LastActivityAt = l.clock.Now()

	ttl := req.LeaseTTL.Duration()
	if ttl == 0 {
		ttl = l.cfg.DefaultTTL.Duration()
	}
	if ttl > l.cfg.MaxTTL.Duration() {
		ttl = l.cfg.MaxTTL.Duration()
	}

	if l.owner == nil {
		leaseID := generateID()
		now := l.clock.Now()
		expiresAt := now.Add(ttl)
		l.fenceToken++
		l.owner = &Lease{
			LeaseID:    leaseID,
			ClientID:   req.ClientID,
			AcquiredAt: now,
			ExpiresAt:  expiresAt,
			FenceToken: l.fenceToken,
		}

		l.metrics.SuccessAcquires++
		l.metrics.OwnerChanges++
		if !l.metrics.uniqueOwnerIDs[req.ClientID] {
			l.metrics.uniqueOwnerIDs[req.ClientID] = true
			l.metrics.UniqueOwners = len(l.metrics.uniqueOwnerIDs)
		}

		resp.State = "granted"
		resp.LeaseID = leaseID
		resp.OwnerClientID = req.ClientID
		resp.ExpiresAt = expiresAt
		resp.FenceToken = l.fenceToken
	} else if req.Wait {
		if l.cfg.MaxQueue > 0 && len(l.queue) >= l.cfg.MaxQueue {
			l.metrics.FailedAcquires++
			resp.State = "busy"
			resp.Err = ErrQueueFull
		} else {
			waitID := generateID()
			timeout := req.Timeout.Duration()
			if timeout == 0 {
				timeout = l.cfg.DefaultTimeout.Duration()
			}
			if timeout == 0 {
				timeout = 0 // no queue timeout fallback at lock level; shard provides default on creation
			}
			entry := queueEntry{
				waitID:    waitID,
				clientID:  req.ClientID,
				timeoutAt: l.clock.Now().Add(timeout),
				addedAt:   l.clock.Now(),
			}
			l.queue = append(l.queue, entry)
			l.metrics.TotalQueued++

			resp.State = "queued"
			resp.WaitID = waitID
			resp.Position = len(l.queue)
			resp.EstimatedWaitMs = int64(l.owner.ExpiresAt.Sub(l.clock.Now()).Milliseconds())
		}
	} else {
		l.metrics.FailedAcquires++
		resp.State = "busy"
		resp.OwnerClientID = l.owner.ClientID
		resp.ExpiresAt = l.owner.ExpiresAt
		resp.Err = ErrLockBusy
	}

	if req.IdemKey != "" {
		l.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: l.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (l *Lock) handleRefresh(req RefreshReq) {
	if req.IdemKey != "" {
		if cached, ok := l.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(l.clock.Now()) {
			req.Reply <- cached.response.(RefreshResp)
			return
		}
	}

	resp := RefreshResp{LockID: req.LockID}

	if l.owner == nil {
		resp.Err = ErrInvalidLease
		req.Reply <- resp
		return
	}

	if l.owner.LeaseID != req.LeaseID || l.owner.ClientID != req.ClientID {
		resp.Err = ErrNotOwner
		req.Reply <- resp
		return
	}

	l.metrics.TotalRefreshes++
	l.metrics.LastActivityAt = l.clock.Now()

	ttl := req.NewTTL.Duration()
	if ttl == 0 {
		ttl = l.cfg.DefaultTTL.Duration()
	}
	if ttl > l.cfg.MaxTTL.Duration() {
		ttl = l.cfg.MaxTTL.Duration()
	}

	newExpiry := l.clock.Now().Add(ttl)
	l.owner.ExpiresAt = newExpiry

	resp.State = "granted"
	resp.LeaseID = req.LeaseID
	resp.ExpiresAt = newExpiry
	resp.FenceToken = l.owner.FenceToken

	if req.IdemKey != "" {
		l.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: l.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (l *Lock) handleRelease(req ReleaseReq) {
	if req.IdemKey != "" {
		if cached, ok := l.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(l.clock.Now()) {
			req.Reply <- cached.response.(ReleaseResp)
			return
		}
	}

	resp := ReleaseResp{LockID: req.LockID, State: "released"}

	if l.owner == nil {
		resp.Err = ErrInvalidLease
		req.Reply <- resp
		return
	}

	if l.owner.LeaseID != req.LeaseID || l.owner.ClientID != req.ClientID {
		resp.Err = ErrNotOwner
		req.Reply <- resp
		return
	}

	now := l.clock.Now()
	holdDuration := now.Sub(l.owner.AcquiredAt)
	holdNs := holdDuration.Nanoseconds()

	l.metrics.TotalReleases++
	l.metrics.LastActivityAt = now
	l.metrics.TotalHoldTimeNs += holdNs
	l.metrics.totalHoldCount++
	if l.metrics.totalHoldCount > 0 {
		l.metrics.AvgHoldTimeNs = l.metrics.TotalHoldTimeNs / l.metrics.totalHoldCount
	}
	if holdNs > l.metrics.MaxHoldTimeNs {
		l.metrics.MaxHoldTimeNs = holdNs
	}

	l.owner = nil

	if promoted := l.promoteFromQueue(); promoted != nil {
		resp.NextOwnerClientID = promoted.ClientID
		l.metrics.OwnerChanges++
	}

	if req.IdemKey != "" {
		l.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: l.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (l *Lock) handleStatus(req StatusReq) {
	resp := StatusResp{}

	resp.Status = LockStatus{
		LockID:   req.LockID,
		Owner:    l.owner,
		QueueLen: len(l.queue),
		Config:   l.cfg,
		Tags:     l.tags,
	}

	req.Reply <- resp
}

func (l *Lock) handleQueueGet(req QueueGetReq) {
	resp := QueueGetResp{LockID: req.LockID, Queue: make([]WaitEntryView, 0)}

	limit := req.Limit
	if limit <= 0 || limit > len(l.queue) {
		limit = len(l.queue)
	}

	for i := 0; i < limit; i++ {
		entry := l.queue[i]
		resp.Queue = append(resp.Queue, WaitEntryView{
			WaitID:    entry.waitID,
			ClientID:  entry.clientID,
			Position:  i + 1,
			TimeoutAt: entry.timeoutAt,
		})
	}

	req.Reply <- resp
}

func (l *Lock) handleQueueDel(req QueueDelReq) {
	resp := QueueDelResp{Removed: false}

	for i, entry := range l.queue {
		if entry.waitID == req.WaitID && entry.clientID == req.ClientID {
			l.queue = append(l.queue[:i], l.queue[i+1:]...)
			resp.Removed = true
			break
		}
	}

	if !resp.Removed {
		resp.Err = ErrWaitNotFound
	}

	req.Reply <- resp
}

func (l *Lock) handleMetrics(req MetricsReq) {
	resp := MetricsResp{LockID: req.LockID}

	if l.metrics == nil {
		now := l.clock.Now()
		l.metrics = &LockMetrics{
			CreatedAt:      l.createdAt,
			LastActivityAt: now,
			uniqueOwnerIDs: make(map[string]bool),
		}
	}

	metrics := *l.metrics
	metrics.CurrentQueueLen = len(l.queue)

	if l.owner != nil {
		now := l.clock.Now()
		currentHoldDuration := now.Sub(l.owner.AcquiredAt)
		metrics.CurrentHoldTimeNs = currentHoldDuration.Nanoseconds()
	} else {
		metrics.CurrentHoldTimeNs = 0
	}

	resp.Metrics = metrics
	req.Reply <- resp
}

func (l *Lock) promoteFromQueue() *Lease {
	if len(l.queue) == 0 {
		return nil
	}

	var entry queueEntry
	if l.cfg.QueueType == QueueTypeLIFO {
		entry = l.queue[len(l.queue)-1]
		l.queue = l.queue[:len(l.queue)-1]
	} else {
		entry = l.queue[0]
		l.queue = l.queue[1:]
	}

	now := l.clock.Now()
	queueDuration := now.Sub(entry.addedAt)
	queueNs := queueDuration.Nanoseconds()

	l.metrics.totalQueueTimeNs += queueNs
	l.metrics.queuePromotions++
	if l.metrics.queuePromotions > 0 {
		l.metrics.AvgQueueTimeNs = l.metrics.totalQueueTimeNs / l.metrics.queuePromotions
	}

	ttl := l.cfg.DefaultTTL.Duration()
	leaseID := generateID()
	expiresAt := now.Add(ttl)
	l.fenceToken++

	l.owner = &Lease{
		LeaseID:    leaseID,
		ClientID:   entry.clientID,
		AcquiredAt: now,
		ExpiresAt:  expiresAt,
		FenceToken: l.fenceToken,
	}

	if !l.metrics.uniqueOwnerIDs[entry.clientID] {
		l.metrics.uniqueOwnerIDs[entry.clientID] = true
		l.metrics.UniqueOwners = len(l.metrics.uniqueOwnerIDs)
	}

	return l.owner
}
