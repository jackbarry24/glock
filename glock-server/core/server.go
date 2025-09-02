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
	Size     int64
	Capacity int
	Locks    sync.Map // Name -> *Lock
	Config   *Config
}

// generateRequestID creates a unique request ID for queue entries
func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (g *GlockServer) CreateLock(req *CreateRequest) (*Lock, int, error) {
	// Parse TTL with default fallback
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

	// Parse MaxTTL with default fallback
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

	// Parse QueueTimeout with default fallback
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

	// Validate queue parameters
	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
			// Valid queue type
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("queue_type must be one of: none, fifo, lifo")
		}
	}

	// Atomically check and increment capacity
	for {
		currentSize := atomic.LoadInt64(&g.Size)
		if currentSize >= int64(g.Capacity) {
			return nil, http.StatusTooManyRequests, fmt.Errorf("lock capacity reached")
		}
		if atomic.CompareAndSwapInt64(&g.Size, currentSize, currentSize+1) {
			break
		}
	}

	// Set defaults for queue behavior
	queueType := req.QueueType
	if queueType == "" {
		queueType = QueueNone
	}

	lock := &Lock{
		Name:         req.Name,
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
		queue:        NewLockQueue(),
	}

	if _, loaded := g.Locks.LoadOrStore(req.Name, lock); loaded {
		// If lock already exists, decrement the size we just incremented
		atomic.AddInt64(&g.Size, -1)
		return nil, http.StatusConflict, fmt.Errorf("lock %s already exists", req.Name)
	}

	return lock, http.StatusOK, nil
}

func (g *GlockServer) UpdateLock(req *UpdateRequest) (*Lock, int, error) {
	// Parse TTL if provided
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

	// Parse MaxTTL if provided
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

	// Parse QueueTimeout if provided
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

	// Validate queue parameters
	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
			// Valid queue type
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("queue_type must be one of: none, fifo, lifo")
		}
	}

	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock %s does not exist", req.Name)
	}
	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if !lock.IsAvailable() {
		return nil, http.StatusConflict, fmt.Errorf("cannot update lock that is currently held")
	}

	// Update TTL if provided
	if ttlDuration > 0 {
		lock.TTL = ttlDuration
	}
	// Update MaxTTL if provided
	if maxTTLDuration > 0 {
		lock.MaxTTL = maxTTLDuration
	}
	// Update metadata if provided
	if req.Metadata != nil {
		lock.Metadata = req.Metadata
	}

	// Update queue settings if provided
	if req.QueueType != "" {
		lock.QueueType = req.QueueType
	}
	if queueTimeoutDuration > 0 {
		lock.QueueTimeout = queueTimeoutDuration
	}

	return lock, http.StatusOK, nil
}

func (g *GlockServer) DeleteLock(name string) (bool, int, error) {
	lockVal, exists := g.Locks.Load(name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if !lock.IsAvailable() {
		return false, http.StatusConflict, fmt.Errorf("cannot delete lock that is currently held")
	}
	actual, loaded := g.Locks.LoadAndDelete(name)
	if loaded && actual == lockVal {
		atomic.AddInt64(&g.Size, -1)
		return true, http.StatusOK, nil
	}

	return false, http.StatusNotFound, fmt.Errorf("lock not found")
}

func (g *GlockServer) AcquireLock(req *AcquireRequest) (interface{}, int, error) {
	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)

	// Clean expired queue entries first
	if lock.queue != nil {
		lock.queue.CleanExpired(time.Now())
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.IsAvailable() {
		// Lock is available, acquire it immediately
		lock.Owner = req.Owner
		lock.OwnerID = req.OwnerID
		lock.AcquiredAt = time.Now()
		lock.LastRefresh = time.Now()
		lock.Available = false
		lock.Token++

		return lock, http.StatusOK, nil
	}

	// Lock is not available, check queue behavior
	if lock.QueueType == QueueNone {
		return nil, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}

	// Add to queue
	requestID := generateRequestID()
	queueReq := &QueueRequest{
		ID:        requestID,
		Name:      req.Name,
		Owner:     req.Owner,
		OwnerID:   req.OwnerID,
		QueuedAt:  time.Now(),
		TimeoutAt: time.Now().Add(lock.QueueTimeout),
	}

	isLIFO := lock.QueueType == QueueLIFO
	lock.queue.Enqueue(queueReq, isLIFO)
	position := lock.queue.GetPosition(requestID)

	queueResp := &QueueResponse{
		RequestID: requestID,
		Position:  position,
	}

	return queueResp, http.StatusAccepted, nil
}

func (g *GlockServer) PollQueue(req *PollRequest) (*PollResponse, int, error) {
	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return &PollResponse{Status: "not_found"}, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)

	lock.mu.Lock()
	defer lock.mu.Unlock()

	// Clean expired queue entries (but not in PollQueue to allow detection of expired requests)
	// if lock.queue != nil {
	// 	lock.queue.CleanExpired(time.Now())
	// }

	// Check if the lock is already assigned to this requester (happened in ReleaseLock)
	if !lock.IsAvailable() && lock.OwnerID == req.OwnerID {
		return &PollResponse{
			Status: "ready",
			Lock:   lock,
		}, http.StatusOK, nil
	}

	// Check if lock is now available and we're first in queue
	if lock.IsAvailable() && lock.queue != nil {
		nextReq := lock.queue.GetNext()
		if nextReq != nil && nextReq.ID == req.RequestID && nextReq.OwnerID == req.OwnerID {
			// Check if the request has expired
			if time.Now().After(nextReq.TimeoutAt) {
				// Request expired, remove it and return expired
				lock.queue.Remove(req.RequestID)
				return &PollResponse{Status: "expired"}, http.StatusGone, nil
			}

			// Remove from queue and acquire lock
			lock.queue.Dequeue()

			lock.Owner = nextReq.Owner
			lock.OwnerID = nextReq.OwnerID
			lock.AcquiredAt = time.Now()
			lock.LastRefresh = time.Now()
			lock.Available = false
			lock.Token++

			return &PollResponse{
				Status: "ready",
				Lock:   lock,
			}, http.StatusOK, nil
		}
	}

	// Check if our request is still in queue
	if lock.queue != nil {
		element, exists := lock.queue.requests[req.RequestID]
		if exists && element != nil {
			queueReq := element.Value.(*QueueRequest)
			position := lock.queue.GetPosition(req.RequestID)

			// Check if our request has expired
			if time.Now().After(queueReq.TimeoutAt) {
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

func (g *GlockServer) RefreshLock(req *RefreshRequest) (*Lock, int, error) {
	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return nil, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.OwnerID != req.OwnerID {
		return nil, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}
	now := time.Now()
	if now.After(lock.AcquiredAt.Add(lock.MaxTTL)) {
		return nil, http.StatusConflict, fmt.Errorf("lock has exceeded max ttl")
	}
	if now.After(lock.LastRefresh.Add(lock.TTL)) {
		return nil, http.StatusConflict, fmt.Errorf("lock ttl has expired")
	}
	if lock.Token > req.Token {
		return nil, http.StatusConflict, fmt.Errorf("another client has acquired this lock")
	}
	lock.LastRefresh = now

	return lock, http.StatusOK, nil
}

func (g *GlockServer) VerifyLock(req *VerifyRequest) (bool, int, error) {
	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)
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

func (g *GlockServer) ReleaseLock(req *ReleaseRequest) (bool, int, error) {
	lockVal, exists := g.Locks.Load(req.Name)
	if !exists {
		return false, http.StatusNotFound, fmt.Errorf("lock not found")
	}
	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.OwnerID != req.OwnerID {
		return false, http.StatusConflict, fmt.Errorf("lock is held by another owner")
	}
	if lock.Token > req.Token {
		return false, http.StatusConflict, fmt.Errorf("another client has acquired this lock")
	}

	// Check if there's someone waiting in queue
	var nextOwner *QueueRequest
	if lock.queue != nil {
		// Find the next non-expired request without removing expired ones
		if lock.queue.Size() > 0 {
			// Peek at the front of the queue
			if front := lock.queue.list.Front(); front != nil {
				candidate := front.Value.(*QueueRequest)
				if time.Now().Before(candidate.TimeoutAt) {
					// Request is not expired, dequeue it
					nextOwner = lock.queue.Dequeue()
				}
				// If expired, leave it in queue so polling can detect it
			}
		}
	}

	newLock := Lock{
		Name:         lock.Name,
		Owner:        "",
		OwnerID:      "",
		AcquiredAt:   time.Time{},
		LastRefresh:  time.Time{},
		Available:    true,
		Token:        lock.Token, // Preserve the fencing token
		TTL:          lock.TTL,
		MaxTTL:       lock.MaxTTL,
		Metadata:     lock.Metadata,
		QueueType:    lock.QueueType,
		QueueTimeout: lock.QueueTimeout,
		queue:        lock.queue, // Transfer the queue
		mu:           sync.Mutex{},
	}

	// If there's someone in queue, immediately assign the lock to them
	if nextOwner != nil {
		newLock.Owner = nextOwner.Owner
		newLock.OwnerID = nextOwner.OwnerID
		newLock.AcquiredAt = time.Now()
		newLock.LastRefresh = time.Now()
		newLock.Available = false
		newLock.Token++
	}

	g.Locks.Store(req.Name, &newLock)
	return true, http.StatusOK, nil
}
