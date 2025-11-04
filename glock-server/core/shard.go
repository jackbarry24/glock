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
		locks:    make(map[string]*Lock),
		idemKeys: make(map[string]*idemEntry),
	}
}

func (sh *Shard) run() {
	ticker := time.NewTicker(sh.cfg.ExpiryTick)
	defer ticker.Stop()

	for {
		select {
		case <-sh.stopCh:
			// stop all locks gracefully
			for _, lk := range sh.locks {
				done := make(chan struct{})
				lk.msgCh <- lockStop{Reply: done}
				<-done
			}
			return
		case <-ticker.C:
			// drive expirations in locks
			for _, lk := range sh.locks {
				select {
				case lk.msgCh <- lockTick{}:
				default:
					// skip if the lock's mailbox is full
				}
			}
			// expire shard-level idempotency entries (used for Create)
			now := sh.clock.Now()
			for key, entry := range sh.idemKeys {
				if entry.expiresAt.Before(now) {
					delete(sh.idemKeys, key)
				}
			}
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

// processExpiries performs a tick across all locks and cleans shard-level idempotency.
func (sh *Shard) processExpiries() {
	now := sh.clock.Now()
	for _, lk := range sh.locks {
		done := make(chan struct{})
		lk.msgCh <- lockTick{Reply: done}
		<-done
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
		if req.Config.QueueTimeout.Duration() == 0 && sh.cfg.DefaultQueueTimeout > 0 {
			req.Config.QueueTimeout = Duration(sh.cfg.DefaultQueueTimeout)
		}

		if req.Config.DefaultTTL.Duration() == 0 && sh.cfg.DefaultTTL > 0 {
			req.Config.DefaultTTL = Duration(sh.cfg.DefaultTTL)
		}

		lk := newLock(req.LockID, req.Config, req.Tags, sh.clock)
		sh.locks[req.LockID] = lk
		go lk.run()
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
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- AcquireResp{LockID: req.LockID, State: "not_found", Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleRefresh(req RefreshReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- RefreshResp{LockID: req.LockID, Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleRelease(req ReleaseReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- ReleaseResp{LockID: req.LockID, State: "released", Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleStatus(req StatusReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- StatusResp{Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleQueueGet(req QueueGetReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- QueueGetResp{LockID: req.LockID, Queue: nil, Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleQueueDel(req QueueDelReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- QueueDelResp{Removed: false, Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleDelete(req DeleteReq) {
	resp := DeleteResp{
		LockID:  req.LockID,
		Deleted: false,
	}

	if lk, exists := sh.locks[req.LockID]; exists {
		// Stop the lock actor before removing it to avoid goroutine leaks
		done := make(chan struct{})
		lk.msgCh <- lockStop{Reply: done}
		<-done
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

// promotions handled inside Lock actor

func (sh *Shard) handleMetrics(req MetricsReq) {
	lock, exists := sh.locks[req.LockID]
	if !exists {
		req.Reply <- MetricsResp{LockID: req.LockID, Err: ErrLockNotFound}
		return
	}
	lock.msgCh <- req
}

func (sh *Shard) handleAllStatus(req AllStatusReq) {
	resp := AllStatusResp{
		Statuses: make([]LockStatus, 0, len(sh.locks)),
	}

	for lockID, lock := range sh.locks {
		reply := make(chan StatusResp, 1)
		lock.msgCh <- StatusReq{LockID: lockID, Reply: reply}
		s := <-reply
		if s.Err != nil {
			continue
		}
		resp.Statuses = append(resp.Statuses, s.Status)
	}

	req.Reply <- resp
}
