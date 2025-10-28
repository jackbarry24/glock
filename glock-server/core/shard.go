package core

import (
	"errors"
	"time"
)

var (
	ErrLockNotFound    = errors.New("lock not found")
	ErrLockExists      = errors.New("lock already exists")
	ErrLockBusy        = errors.New("lock is currently held")
	ErrInvalidLease    = errors.New("invalid lease")
	ErrQueueFull       = errors.New("queue is full")
	ErrNotOwner        = errors.New("not the lock owner")
	ErrWaitNotFound    = errors.New("wait entry not found")
	ErrCapacityReached = errors.New("server capacity reached")
)

func newShard(id int, cfg ServerConfig, clock Clock) *Shard {
	return &Shard{
		id:       id,
		cfg:      cfg,
		clock:    clock,
		msgCh:    make(chan Msg, 1000),
		stopCh:   make(chan struct{}),
		locks:    make(map[string]*lockState),
		idemKeys: make(map[string]*idemEntry),
	}
}

func (sh *Shard) run() {
	ticker := time.NewTicker(sh.cfg.ExpiryTick)
	defer ticker.Stop()

	for {
		select {
		case <-sh.stopCh:
			return
		case <-ticker.C:
			sh.processExpiries()
		case msg := <-sh.msgCh:
			sh.processMessage(msg)
		}
	}
}

func (sh *Shard) stop() {
	close(sh.stopCh)
}

func (sh *Shard) processMessage(msg Msg) {
	switch m := msg.(type) {
	case AcquireReq:
		sh.handleAcquire(m)
	case RefreshReq:
		sh.handleRefresh(m)
	case ReleaseReq:
		sh.handleRelease(m)
	case CreateReq:
		sh.handleCreate(m)
	case StatusReq:
		sh.handleStatus(m)
	case QueueGetReq:
		sh.handleQueueGet(m)
	case QueueDelReq:
		sh.handleQueueDel(m)
	case DeleteReq:
		sh.handleDelete(m)
	case listReq:
		sh.handleList(m)
	case MetricsReq:
		sh.handleMetrics(m)
	case AllStatusReq:
		sh.handleAllStatus(m)
	}
}

func (sh *Shard) processExpiries() {
	now := sh.clock.Now()

	for lockID, lock := range sh.locks {
		if lock.owner != nil && lock.owner.ExpiresAt.Before(now) {
			holdDuration := now.Sub(lock.owner.AcquiredAt)
			holdNs := holdDuration.Nanoseconds()

			lock.metrics.TotalHoldTimeNs += holdNs
			lock.metrics.totalHoldCount++
			if lock.metrics.totalHoldCount > 0 {
				lock.metrics.AvgHoldTimeNs = lock.metrics.TotalHoldTimeNs / lock.metrics.totalHoldCount
			}
			if holdNs > lock.metrics.MaxHoldTimeNs {
				lock.metrics.MaxHoldTimeNs = holdNs
			}
			lock.metrics.TTLExpirations++

			lock.owner = nil
			sh.promoteFromQueue(lockID, lock)
		}

		for i := len(lock.queue) - 1; i >= 0; i-- {
			if lock.queue[i].timeoutAt.Before(now) {
				lock.metrics.QueueTimeouts++
				lock.queue = append(lock.queue[:i], lock.queue[i+1:]...)
			}
		}
	}

	for key, entry := range sh.idemKeys {
		if entry.expiresAt.Before(now) {
			delete(sh.idemKeys, key)
		}
	}
}

func (sh *Shard) handleCreate(req CreateReq) {
	if req.IdemKey != "" {
		if cached, ok := sh.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(sh.clock.Now()) {
			req.Reply <- cached.response.(CreateResp)
			return
		}
	}

	resp := CreateResp{
		LockID: req.LockID,
		Config: req.Config,
	}

	if _, exists := sh.locks[req.LockID]; exists {
		resp.Err = ErrLockExists
		resp.Created = false
	} else if sh.cfg.Capacity > 0 && len(sh.locks) >= sh.cfg.Capacity {
		resp.Err = ErrCapacityReached
		resp.Created = false
	} else {
		tags := req.Tags
		if len(tags) > 16 {
			tags = tags[:16]
		}

		now := sh.clock.Now()
		sh.locks[req.LockID] = &lockState{
			cfg:       req.Config,
			owner:     nil,
			queue:     make([]queueEntry, 0),
			createdAt: now,
			metrics: &LockMetrics{
				CreatedAt:      now,
				LastActivityAt: now,
				uniqueOwnerIDs: make(map[string]bool),
			},
			tags: tags,
		}
		resp.Created = true
	}

	if req.IdemKey != "" {
		sh.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: sh.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (sh *Shard) handleAcquire(req AcquireReq) {
	if req.IdemKey != "" {
		if cached, ok := sh.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(sh.clock.Now()) {
			req.Reply <- cached.response.(AcquireResp)
			return
		}
	}

	resp := AcquireResp{
		LockID: req.LockID,
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		if sh.cfg.Capacity > 0 && len(sh.locks) >= sh.cfg.Capacity {
			resp.State = "busy"
			resp.Err = ErrCapacityReached
			req.Reply <- resp
			return
		}

		now := sh.clock.Now()
		lock = &lockState{
			cfg: LockConfig{
				MaxTTL:         Duration(sh.cfg.DefaultMaxTTL),
				DefaultTTL:     Duration(sh.cfg.DefaultTTL),
				MaxQueue:       sh.cfg.DefaultQueue,
				QueueType:      QueueTypeFIFO,
				DefaultTimeout: Duration(sh.cfg.DefaultQueueTimeout),
			},
			owner:     nil,
			queue:     make([]queueEntry, 0),
			createdAt: now,
			metrics: &LockMetrics{
				CreatedAt:      now,
				LastActivityAt: now,
				uniqueOwnerIDs: make(map[string]bool),
			},
		}
		sh.locks[req.LockID] = lock
	}

	lock.metrics.TotalAcquires++
	lock.metrics.LastActivityAt = sh.clock.Now()

	ttl := req.LeaseTTL.Duration()
	if ttl == 0 {
		ttl = lock.cfg.DefaultTTL.Duration()
	}
	if ttl > lock.cfg.MaxTTL.Duration() {
		ttl = lock.cfg.MaxTTL.Duration()
	}

	if lock.owner == nil {
		leaseID := generateID()
		now := sh.clock.Now()
		expiresAt := now.Add(ttl)
		lock.fenceToken++
		lock.owner = &Lease{
			LeaseID:    leaseID,
			ClientID:   req.ClientID,
			AcquiredAt: now,
			ExpiresAt:  expiresAt,
			FenceToken: lock.fenceToken,
		}

		lock.metrics.SuccessAcquires++
		lock.metrics.OwnerChanges++
		if !lock.metrics.uniqueOwnerIDs[req.ClientID] {
			lock.metrics.uniqueOwnerIDs[req.ClientID] = true
			lock.metrics.UniqueOwners = len(lock.metrics.uniqueOwnerIDs)
		}

		resp.State = "granted"
		resp.LeaseID = leaseID
		resp.OwnerClientID = req.ClientID
		resp.ExpiresAt = expiresAt
		resp.FenceToken = lock.fenceToken
	} else if req.Wait {
		if lock.cfg.MaxQueue > 0 && len(lock.queue) >= lock.cfg.MaxQueue {
			lock.metrics.FailedAcquires++
			resp.State = "busy"
			resp.Err = ErrQueueFull
		} else {
			waitID := generateID()
			timeout := req.Timeout.Duration()
			if timeout == 0 {
				timeout = lock.cfg.DefaultTimeout.Duration()
				if timeout == 0 {
					timeout = sh.cfg.DefaultQueueTimeout
				}
			}

			entry := queueEntry{
				waitID:    waitID,
				clientID:  req.ClientID,
				timeoutAt: sh.clock.Now().Add(timeout),
				addedAt:   sh.clock.Now(),
			}
			lock.queue = append(lock.queue, entry)
			lock.metrics.TotalQueued++

			resp.State = "queued"
			resp.WaitID = waitID
			resp.Position = len(lock.queue)
			resp.EstimatedWaitMs = int64(lock.owner.ExpiresAt.Sub(sh.clock.Now()).Milliseconds())
		}
	} else {
		lock.metrics.FailedAcquires++
		resp.State = "busy"
		resp.OwnerClientID = lock.owner.ClientID
		resp.ExpiresAt = lock.owner.ExpiresAt
		resp.Err = ErrLockBusy
	}

	if req.IdemKey != "" {
		sh.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: sh.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (sh *Shard) handleRefresh(req RefreshReq) {
	if req.IdemKey != "" {
		if cached, ok := sh.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(sh.clock.Now()) {
			req.Reply <- cached.response.(RefreshResp)
			return
		}
	}

	resp := RefreshResp{
		LockID: req.LockID,
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	if lock.owner == nil {
		resp.Err = ErrInvalidLease
		req.Reply <- resp
		return
	}

	if lock.owner.LeaseID != req.LeaseID || lock.owner.ClientID != req.ClientID {
		resp.Err = ErrNotOwner
		req.Reply <- resp
		return
	}

	lock.metrics.TotalRefreshes++
	lock.metrics.LastActivityAt = sh.clock.Now()

	ttl := req.NewTTL.Duration()
	if ttl == 0 {
		ttl = lock.cfg.DefaultTTL.Duration()
	}
	if ttl > lock.cfg.MaxTTL.Duration() {
		ttl = lock.cfg.MaxTTL.Duration()
	}

	newExpiry := sh.clock.Now().Add(ttl)
	lock.owner.ExpiresAt = newExpiry

	resp.State = "granted"
	resp.LeaseID = req.LeaseID
	resp.ExpiresAt = newExpiry
	resp.FenceToken = lock.owner.FenceToken

	if req.IdemKey != "" {
		sh.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: sh.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (sh *Shard) handleRelease(req ReleaseReq) {
	if req.IdemKey != "" {
		if cached, ok := sh.idemKeys[req.IdemKey]; ok && cached.expiresAt.After(sh.clock.Now()) {
			req.Reply <- cached.response.(ReleaseResp)
			return
		}
	}

	resp := ReleaseResp{
		LockID: req.LockID,
		State:  "released",
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	if lock.owner == nil {
		resp.Err = ErrInvalidLease
		req.Reply <- resp
		return
	}

	if lock.owner.LeaseID != req.LeaseID || lock.owner.ClientID != req.ClientID {
		resp.Err = ErrNotOwner
		req.Reply <- resp
		return
	}

	now := sh.clock.Now()
	holdDuration := now.Sub(lock.owner.AcquiredAt)
	holdNs := holdDuration.Nanoseconds()

	lock.metrics.TotalReleases++
	lock.metrics.LastActivityAt = now
	lock.metrics.TotalHoldTimeNs += holdNs
	lock.metrics.totalHoldCount++
	if lock.metrics.totalHoldCount > 0 {
		lock.metrics.AvgHoldTimeNs = lock.metrics.TotalHoldTimeNs / lock.metrics.totalHoldCount
	}
	if holdNs > lock.metrics.MaxHoldTimeNs {
		lock.metrics.MaxHoldTimeNs = holdNs
	}

	lock.owner = nil

	if promoted := sh.promoteFromQueue(req.LockID, lock); promoted != nil {
		resp.NextOwnerClientID = promoted.ClientID
		lock.metrics.OwnerChanges++
	}

	if req.IdemKey != "" {
		sh.idemKeys[req.IdemKey] = &idemEntry{
			response:  resp,
			expiresAt: sh.clock.Now().Add(24 * time.Hour),
		}
	}

	req.Reply <- resp
}

func (sh *Shard) handleStatus(req StatusReq) {
	resp := StatusResp{}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	resp.Status = LockStatus{
		LockID:   req.LockID,
		Owner:    lock.owner,
		QueueLen: len(lock.queue),
		Config:   lock.cfg,
		Tags:     lock.tags,
	}

	req.Reply <- resp
}

func (sh *Shard) handleQueueGet(req QueueGetReq) {
	resp := QueueGetResp{
		LockID: req.LockID,
		Queue:  make([]WaitEntryView, 0),
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	limit := req.Limit
	if limit <= 0 || limit > len(lock.queue) {
		limit = len(lock.queue)
	}

	for i := 0; i < limit; i++ {
		entry := lock.queue[i]
		resp.Queue = append(resp.Queue, WaitEntryView{
			WaitID:    entry.waitID,
			ClientID:  entry.clientID,
			Position:  i + 1,
			TimeoutAt: entry.timeoutAt,
		})
	}

	req.Reply <- resp
}

func (sh *Shard) handleQueueDel(req QueueDelReq) {
	resp := QueueDelResp{
		Removed: false,
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	for i, entry := range lock.queue {
		if entry.waitID == req.WaitID && entry.clientID == req.ClientID {
			lock.queue = append(lock.queue[:i], lock.queue[i+1:]...)
			resp.Removed = true
			break
		}
	}

	if !resp.Removed {
		resp.Err = ErrWaitNotFound
	}

	req.Reply <- resp
}

func (sh *Shard) handleDelete(req DeleteReq) {
	resp := DeleteResp{
		LockID:  req.LockID,
		Deleted: false,
	}

	if _, exists := sh.locks[req.LockID]; exists {
		delete(sh.locks, req.LockID)
		resp.Deleted = true
	} else {
		resp.Err = ErrLockNotFound
	}

	req.Reply <- resp
}

func (sh *Shard) handleList(req listReq) {
	lockIDs := make([]string, 0, len(sh.locks))
	for lockID := range sh.locks {
		lockIDs = append(lockIDs, lockID)
	}
	req.Reply <- lockIDs
}

func (sh *Shard) promoteFromQueue(lockID string, lock *lockState) *Lease {
	if len(lock.queue) == 0 {
		return nil
	}

	var entry queueEntry

	if lock.cfg.QueueType == QueueTypeLIFO {
		entry = lock.queue[len(lock.queue)-1]
		lock.queue = lock.queue[:len(lock.queue)-1]
	} else {
		entry = lock.queue[0]
		lock.queue = lock.queue[1:]
	}

	now := sh.clock.Now()
	queueDuration := now.Sub(entry.addedAt)
	queueNs := queueDuration.Nanoseconds()

	lock.metrics.totalQueueTimeNs += queueNs
	lock.metrics.queuePromotions++
	if lock.metrics.queuePromotions > 0 {
		lock.metrics.AvgQueueTimeNs = lock.metrics.totalQueueTimeNs / lock.metrics.queuePromotions
	}

	ttl := lock.cfg.DefaultTTL.Duration()
	leaseID := generateID()
	expiresAt := now.Add(ttl)
	lock.fenceToken++

	lock.owner = &Lease{
		LeaseID:    leaseID,
		ClientID:   entry.clientID,
		AcquiredAt: now,
		ExpiresAt:  expiresAt,
		FenceToken: lock.fenceToken,
	}

	if !lock.metrics.uniqueOwnerIDs[entry.clientID] {
		lock.metrics.uniqueOwnerIDs[entry.clientID] = true
		lock.metrics.UniqueOwners = len(lock.metrics.uniqueOwnerIDs)
	}

	return lock.owner
}

func (sh *Shard) handleMetrics(req MetricsReq) {
	resp := MetricsResp{
		LockID: req.LockID,
	}

	lock, exists := sh.locks[req.LockID]
	if !exists {
		resp.Err = ErrLockNotFound
		req.Reply <- resp
		return
	}

	if lock.metrics == nil {
		now := sh.clock.Now()
		lock.metrics = &LockMetrics{
			CreatedAt:      lock.createdAt,
			LastActivityAt: now,
			uniqueOwnerIDs: make(map[string]bool),
		}
	}

	metrics := *lock.metrics
	metrics.CurrentQueueLen = len(lock.queue)

	if lock.owner != nil {
		now := sh.clock.Now()
		currentHoldDuration := now.Sub(lock.owner.AcquiredAt)
		metrics.CurrentHoldTimeNs = currentHoldDuration.Nanoseconds()
	} else {
		metrics.CurrentHoldTimeNs = 0
	}

	resp.Metrics = metrics

	req.Reply <- resp
}

func (sh *Shard) handleAllStatus(req AllStatusReq) {
	resp := AllStatusResp{
		Statuses: make([]LockStatus, 0, len(sh.locks)),
	}

	for lockID, lock := range sh.locks {
		resp.Statuses = append(resp.Statuses, LockStatus{
			LockID:   lockID,
			Owner:    lock.owner,
			QueueLen: len(lock.queue),
			Config:   lock.cfg,
			Tags:     lock.tags,
		})
	}

	req.Reply <- resp
}
