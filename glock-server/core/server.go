package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type GlockServer struct {
	Size        int64
	Capacity    int
	Locks       sync.Map // Name -> Tree Node
	LockTree    *LockTree
	Config      *Config
	cleanupDone chan struct{} // Channel to signal cleanup goroutine to stop
}

func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (g *GlockServer) grantLockOwnership(lock *Lock, owner, ownerID string, now time.Time) {
	lock.Owner = owner
	lock.OwnerID = ownerID
	lock.AcquiredAt = now
	lock.LastRefresh = now
	lock.Available = false
	lock.Token++
	lock.recordAcquireAttempt(true)
	lock.recordOwnerChange(owner, ownerID, now, g.Config.OwnerHistoryMaxSize)
}

func (g *GlockServer) CreateLock(req *CreateRequest) (*Lock, int, error) {
	ttlDuration := g.Config.DefaultTTL
	if req.TTL != "" {
		var err error
		ttlDuration, err = time.ParseDuration(req.TTL)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid ttl format: %v", err)
		}
	}
	if ttlDuration <= 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("ttl must be greater than zero")
	}

	maxTTLDuration := g.Config.DefaultMaxTTL
	if req.MaxTTL != "" {
		var err error
		maxTTLDuration, err = time.ParseDuration(req.MaxTTL)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid max_ttl format: %v", err)
		}
	}
	if maxTTLDuration <= 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("max_ttl must be greater than zero")
	}

	if maxTTLDuration < ttlDuration {
		return nil, http.StatusBadRequest, fmt.Errorf("max_ttl must be greater than or equal to ttl")
	}

	queueTimeoutDuration := g.Config.DefaultQueueTimeout
	if req.QueueTimeout != "" {
		var err error
		queueTimeoutDuration, err = time.ParseDuration(req.QueueTimeout)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid queue_timeout format: %v", err)
		}
		if queueTimeoutDuration <= 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("queue_timeout must be greater than zero")
		}
	}

	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("queue_type must be one of: none, fifo, lifo")
		}
	}

	var parentNode *Node
	if req.Parent != "" {
		val, exists := g.Locks.Load(req.Parent)
		if !exists {
			return nil, http.StatusBadRequest, fmt.Errorf("parent lock %s does not exist", req.Parent)
		}
		parentNode = val.(*Node)
	} else {
		parentNode = g.LockTree.Root
	}

	for {
		currentSize := atomic.LoadInt64(&g.Size)
		if currentSize >= int64(g.Capacity) {
			return nil, http.StatusTooManyRequests, fmt.Errorf("lock capacity reached")
		}
		if atomic.CompareAndSwapInt64(&g.Size, currentSize, currentSize+1) {
			break
		}
	}

	queueType := req.QueueType
	if queueType == "" {
		queueType = QueueNone
	}

	lock := &Lock{
		Name:         req.Name,
		Parent:       req.Parent,
		Owner:        "",
		OwnerID:      "",
		AcquiredAt:   time.Time{},
		LastRefresh:  time.Time{},
		Available:    true,
		Token:        0,
		TTL:          ttlDuration,
		MaxTTL:       maxTTLDuration,
		Metadata:     req.Metadata,
		QueueType:    queueType,
		QueueTimeout: queueTimeoutDuration,
		Frozen:       false,
		queue:        NewLockQueue(),
	}

	node := NewNode(parentNode, lock)

	if _, loaded := g.Locks.LoadOrStore(req.Name, node); loaded {
		atomic.AddInt64(&g.Size, -1)
		return nil, http.StatusConflict, fmt.Errorf("lock %s already exists", req.Name)
	}

	lock.QueueSize = lock.getCurrentQueueSize()
	return lock, http.StatusOK, nil
}

func (g *GlockServer) UpdateLock(req *UpdateRequest) (*Lock, int, error) {
	var ttlDuration time.Duration
	if req.TTL != "" {
		var err error
		ttlDuration, err = time.ParseDuration(req.TTL)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid ttl format: %v", err)
		}
		if ttlDuration <= 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("ttl must be greater than zero")
		}
	}

	var maxTTLDuration time.Duration
	if req.MaxTTL != "" {
		var err error
		maxTTLDuration, err = time.ParseDuration(req.MaxTTL)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid max_ttl format: %v", err)
		}
		if maxTTLDuration <= 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("max_ttl must be greater than zero")
		}
	}

	var queueTimeoutDuration time.Duration
	if req.QueueTimeout != "" {
		var err error
		queueTimeoutDuration, err = time.ParseDuration(req.QueueTimeout)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid queue_timeout format: %v", err)
		}
		if queueTimeoutDuration <= 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("queue_timeout must be greater than zero")
		}
	}

	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("queue_type must be one of: none, fifo, lifo")
		}
	}

	nodeVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock %s does not exist", req.Name)
	}
	node := nodeVal.(*Node)
	lock := node.Lock
	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()
	if !lock.IsAvailable(now) {
		return nil, http.StatusConflict, fmt.Errorf("cannot update lock that is currently held")
	}

	if ttlDuration > 0 {
		lock.TTL = ttlDuration
	}
	if maxTTLDuration > 0 {
		lock.MaxTTL = maxTTLDuration
	}
	if req.Metadata != nil {
		lock.Metadata = req.Metadata
	}

	if req.QueueType != "" {
		lock.QueueType = req.QueueType
	}
	if queueTimeoutDuration > 0 {
		lock.QueueTimeout = queueTimeoutDuration
	}

	if req.Parent != "" && req.Parent != lock.Parent {
		val, exists := g.Locks.Load(req.Parent)
		if !exists {
			return nil, http.StatusBadRequest, fmt.Errorf("parent lock %s does not exist", req.Parent)
		}
		newParentNode := val.(*Node)
		if newParentNode.IsAnyParentHeld(now) {
			return nil, http.StatusConflict, fmt.Errorf("cannot change parent: an ancestor lock is currently held")
		}
		lock.Parent = req.Parent
		node.ChangeParent(newParentNode)
	}

	lock.QueueSize = lock.getCurrentQueueSize()
	return lock, http.StatusOK, nil
}

func (g *GlockServer) DeleteLock(name string) (bool, int, error) {
	nodeVal, exists := g.Locks.Load(name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	node := nodeVal.(*Node)
	lock := node.Lock
	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()
	if !lock.IsAvailable(now) {
		return false, http.StatusConflict, fmt.Errorf("cannot delete lock that is currently held")
	}
	if node.IsParentHeld(now) {
		return false, http.StatusConflict, fmt.Errorf("cannot delete lock: parent lock is currently held")
	}

	actual, loaded := g.Locks.LoadAndDelete(name)
	if loaded && actual == node {
		node.Remove()
		atomic.AddInt64(&g.Size, -1)
		return true, http.StatusOK, nil
	}

	return false, http.StatusNotFound, fmt.Errorf("lock not found")
}

func (g *GlockServer) AcquireLock(req *AcquireRequest) (interface{}, int, error) {
	nodeVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	node := nodeVal.(*Node)
	lock := node.Lock

	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()

	if lock.queue != nil {
		lock.queue.CleanExpired(now)
	}

	if lock.Frozen {
		return nil, http.StatusForbidden, fmt.Errorf("lock %s is frozen and cannot be acquired", req.Name)
	}

	if node.IsAnyParentHeld(now) {
		lock.recordAcquireAttempt(false)
		return nil, http.StatusConflict, fmt.Errorf("cannot acquire lock: an ancestor lock is currently held")
	}

	if lock.IsAvailable(now) {
		g.grantLockOwnership(lock, req.Owner, req.OwnerID, now)
		return lock, http.StatusOK, nil
	}

	shouldQueue := true
	if req.QueueRequest != nil {
		shouldQueue = *req.QueueRequest
	}

	if lock.QueueType == QueueNone || !shouldQueue {
		lock.recordAcquireAttempt(false)
		return nil, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}

	currentQueueSize := lock.getCurrentQueueSize()
	if currentQueueSize >= g.Config.QueueMaxSize {
		lock.recordAcquireAttempt(false)
		return nil, http.StatusTooManyRequests, fmt.Errorf("queue is at maximum capacity (%d)", g.Config.QueueMaxSize)
	}

	requestID := generateRequestID()
	queueReq := &QueueRequest{
		ID:        requestID,
		Name:      req.Name,
		Owner:     req.Owner,
		OwnerID:   req.OwnerID,
		QueuedAt:  now,
		TimeoutAt: now.Add(lock.QueueTimeout),
	}

	isLIFO := lock.QueueType == QueueLIFO
	lock.queue.Enqueue(queueReq, isLIFO)
	position := lock.queue.GetPosition(requestID)

	lock.recordAcquireAttempt(false)
	lock.recordQueueRequest()

	if lock.IsAvailable(now) {
		nextReq := lock.queue.GetNext()
		if nextReq != nil && nextReq.ID == requestID {
			lock.queue.Dequeue()
			g.grantLockOwnership(lock, req.Owner, req.OwnerID, now)
			lock.recordQueueTimeout()
			return lock, http.StatusOK, nil
		}
	}

	queueResp := &QueueResponse{
		RequestID: requestID,
		Position:  position,
	}

	return queueResp, http.StatusAccepted, nil
}

func (g *GlockServer) PollQueue(req *PollRequest) (*PollResponse, int, error) {
	node, exists := g.Locks.Load(req.Name)
	if !exists {
		return &PollResponse{Status: "not_found"}, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := node.(*Node).Lock

	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()

	if !lock.IsAvailable(now) && lock.OwnerID == req.OwnerID {
		return &PollResponse{
			Status: "ready",
			Lock:   lock,
		}, http.StatusOK, nil
	}

	if lock.IsAvailable(now) && lock.queue != nil {
		nextReq := lock.queue.GetNext()
		if nextReq != nil && nextReq.ID == req.RequestID && nextReq.OwnerID == req.OwnerID {
			if now.After(nextReq.TimeoutAt) {
				lock.queue.Remove(req.RequestID)
				return &PollResponse{Status: "expired"}, http.StatusGone, nil
			}

			lock.queue.Dequeue()
			g.grantLockOwnership(lock, nextReq.Owner, nextReq.OwnerID, now)

			return &PollResponse{
				Status: "ready",
				Lock:   lock,
			}, http.StatusOK, nil
		}
	}

	if lock.queue != nil {
		element, exists := lock.queue.requests[req.RequestID]
		if exists && element != nil {
			queueReq := element.Value.(*QueueRequest)
			position := lock.queue.GetPosition(req.RequestID)

			if now.After(queueReq.TimeoutAt) {
				lock.queue.Remove(req.RequestID)
				return &PollResponse{Status: "expired"}, http.StatusGone, nil
			}

			return &PollResponse{
				Status:   "waiting",
				Position: position,
			}, http.StatusOK, nil
		}
	}

	return &PollResponse{Status: "not_found"}, http.StatusNotFound, fmt.Errorf("request not found in queue")
}

func (g *GlockServer) RemoveFromQueue(req *PollRequest) (bool, int, error) {
	node, exists := g.Locks.Load(req.Name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := node.(*Node).Lock

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.queue == nil {
		return false, http.StatusNotFound, fmt.Errorf("no queue for this lock")
	}

	// Try to remove the request
	removed := lock.queue.Remove(req.RequestID)
	if removed == nil {
		return false, http.StatusNotFound, fmt.Errorf("request not found in queue")
	}

	return true, http.StatusOK, nil
}

func (g *GlockServer) ListQueue(req *QueueListRequest) (*QueueListResponse, int, error) {
	node, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := node.(*Node).Lock

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.queue == nil {
		return &QueueListResponse{
			LockName: req.Name,
			Requests: []*QueueRequest{},
		}, http.StatusOK, nil
	}

	now := time.Now()
	lock.queue.CleanExpired(now)

	requests := make([]*QueueRequest, 0)
	for e := lock.queue.list.Front(); e != nil; e = e.Next() {
		req := e.Value.(*QueueRequest)
		requests = append(requests, req)
	}

	return &QueueListResponse{
		LockName: req.Name,
		Requests: requests,
	}, http.StatusOK, nil
}

func (g *GlockServer) RefreshLock(req *RefreshRequest) (*Lock, int, error) {
	node, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := node.(*Node).Lock
	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.Frozen {
		lock.recordFailedOperation()
		return nil, http.StatusForbidden, fmt.Errorf("lock %s is frozen and cannot be refreshed", req.Name)
	}

	if lock.OwnerID != req.OwnerID {
		lock.recordFailedOperation()
		return nil, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}
	now := time.Now()
	if now.After(lock.AcquiredAt.Add(lock.MaxTTL)) {
		lock.recordMaxTTLExpiration()
		return nil, http.StatusConflict, fmt.Errorf("lock has exceeded max ttl")
	}
	if now.After(lock.LastRefresh.Add(lock.TTL)) {
		lock.recordTTLExpiration()
		return nil, http.StatusConflict, fmt.Errorf("lock ttl has expired")
	}
	if lock.Token > req.Token {
		lock.recordStaleToken()
		return nil, http.StatusConflict, fmt.Errorf("another client has acquired this lock")
	}
	lock.LastRefresh = now
	lock.recordRefresh()

	lock.QueueSize = lock.getCurrentQueueSize()
	return lock, http.StatusOK, nil
}

func (g *GlockServer) VerifyLock(req *VerifyRequest) (bool, int, error) {
	node, exists := g.Locks.Load(req.Name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := node.(*Node).Lock
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.OwnerID != req.OwnerID {
		return false, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}
	now := time.Now()
	if now.After(lock.AcquiredAt.Add(lock.MaxTTL)) {
		return false, http.StatusConflict, fmt.Errorf("lock has exceeded max ttl")
	}
	if now.After(lock.LastRefresh.Add(lock.TTL)) {
		return false, http.StatusConflict, fmt.Errorf("lock ttl has expired")
	}
	if lock.Token > req.Token {
		return false, http.StatusConflict, fmt.Errorf("another client has acquired this lock")
	}

	return true, http.StatusOK, nil
}

func (g *GlockServer) processQueueForAvailableLock(lock *Lock) bool {
	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()
	if !lock.IsAvailable(now) || lock.queue == nil || lock.queue.Size() == 0 {
		return false
	}

	var nextOwner *QueueRequest
	if front := lock.queue.list.Front(); front != nil {
		candidate := front.Value.(*QueueRequest)
		if now.Before(candidate.TimeoutAt) {
			nextOwner = lock.queue.Dequeue()
		}
	}

	if nextOwner == nil {
		return false
	}

	g.grantLockOwnership(lock, nextOwner.Owner, nextOwner.OwnerID, now)
	lock.recordQueueTimeout()

	return true
}

func (g *GlockServer) StartCleanupGoroutine() {
	g.cleanupDone = make(chan struct{})
	go g.cleanupWorker()
}

func (g *GlockServer) StopCleanupGoroutine() {
	if g.cleanupDone != nil {
		close(g.cleanupDone)
	}
}

func (g *GlockServer) cleanupWorker() {
	ticker := time.NewTicker(g.Config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.processExpiredLocks()
		case <-g.cleanupDone:
			return
		}
	}
}

func (g *GlockServer) processExpiredLocks() {
	g.Locks.Range(func(key, value interface{}) bool {
		lockName := key.(string)
		node := value.(*Node)

		if g.processQueueForAvailableLock(node.Lock) {
			g.Locks.Store(lockName, node)
		}

		if node.Lock.queue != nil {
			node.Lock.queue.CleanExpired(time.Now())
		}

		return true
	})
}

func (g *GlockServer) ReleaseLock(req *ReleaseRequest) (bool, int, error) {
	nodeVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	node := nodeVal.(*Node)
	lock := node.Lock
	lock.mu.Lock()
	defer lock.mu.Unlock()

	now := time.Now()

	if lock.OwnerID != req.OwnerID {
		lock.recordFailedOperation()
		return false, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}
	if lock.Token > req.Token {
		lock.recordStaleToken()
		return false, http.StatusConflict, fmt.Errorf("another client has acquired this lock")
	}

	var nextOwner *QueueRequest
	if lock.queue != nil && lock.queue.Size() > 0 {
		if front := lock.queue.list.Front(); front != nil {
			candidate := front.Value.(*QueueRequest)
			if now.Before(candidate.TimeoutAt) {
				nextOwner = lock.queue.Dequeue()
			}
		}
	}

	lock.recordRelease()

	newLock := Lock{
		Name:         lock.Name,
		Parent:       lock.Parent,
		Owner:        "",
		OwnerID:      "",
		AcquiredAt:   time.Time{},
		LastRefresh:  time.Time{},
		Available:    true,
		Token:        lock.Token,
		TTL:          lock.TTL,
		MaxTTL:       lock.MaxTTL,
		Metadata:     lock.Metadata,
		QueueType:    lock.QueueType,
		QueueTimeout: lock.QueueTimeout,
		queue:        lock.queue,
		mu:           sync.Mutex{},
		metrics:      lock.metrics,
		Frozen:       lock.Frozen,
	}

	if nextOwner != nil {
		g.grantLockOwnership(&newLock, nextOwner.Owner, nextOwner.OwnerID, now)
		newLock.recordQueueTimeout()
	}

	node.Lock = &newLock

	g.Locks.Store(req.Name, node)
	return true, http.StatusOK, nil
}
