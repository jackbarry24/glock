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
	Locks    sync.Map // Name -> Tree Node
	LockTree *LockTree
	Config   *Config
}

func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// reserveLockSlot atomically reserves a slot in the server's capacity.
// Returns an error if capacity is reached.
func (g *GlockServer) reserveLockSlot() error {
	for {
		currentSize := atomic.LoadInt64(&g.Size)
		if currentSize >= int64(g.Capacity) {
			return fmt.Errorf("lock capacity reached")
		}
		if atomic.CompareAndSwapInt64(&g.Size, currentSize, currentSize+1) {
			return nil
		}
	}
}

// releaseLockSlot atomically releases a slot in the server's capacity.
func (g *GlockServer) releaseLockSlot() {
	atomic.AddInt64(&g.Size, -1)
}

// tryGrantFromQueue attempts to grant the lock to the next request in the queue.
// The lock must be available and the caller must hold lock.mu.
// Returns true if a lock was granted from the queue.
// Automatically skips and removes expired requests from the front of the queue.
func (g *GlockServer) tryGrantFromQueue(node *Node, now time.Time) bool {
	lock := node.Lock
	if lock.queue == nil || lock.queue.Size() == 0 {
		return false
	}

	if node.IsAnyParentHeld(now) || node.HasHeldDescendants() {
		return false
	}

	for {
		nextReq := lock.queue.PeekNext()
		if nextReq == nil {
			return false
		}

		// If expired, remove and continue to next
		if now.After(nextReq.TimeoutAt) {
			lock.queue.Dequeue()
			continue
		}

		// Found a valid request, grant the lock
		lock.queue.Dequeue()
		g.grantLockOwnership(node, nextReq.Owner, nextReq.OwnerID, now)
		return true
	}
}

// tryGrantFromRelatedQueues attempts to grant locks to related nodes (ancestors and descendants)
// whose queues may now be unblocked after this node's lock was released.
func (g *GlockServer) tryGrantFromRelatedQueues(node *Node, now time.Time) {
	// Get all related nodes that might be unblocked in one pass
	// Ancestors: may be unblocked if we were blocking them (descendant was held)
	// Descendants: may be unblocked if we were blocking them (ancestor was held)

	ancestors := node.GetAncestors()
	descendants := node.GetAllDescendants()

	// Try ancestors first (typically smaller set due to shallow hierarchies)
	for _, ancestor := range ancestors {
		if ancestor.Lock != nil {
			lock := ancestor.Lock
			lock.mu.Lock()
			g.tryGrantFromQueue(ancestor, now)
			lock.QueueSize = lock.getCurrentQueueSize()
			lock.mu.Unlock()
		}
	}

	// Try all descendants (single DFS traversal, pre-allocated slice)
	for _, descendant := range descendants {
		if descendant.Lock != nil {
			lock := descendant.Lock
			lock.mu.Lock()
			g.tryGrantFromQueue(descendant, now)
			lock.QueueSize = lock.getCurrentQueueSize()
			lock.mu.Unlock()
		}
	}
}

func (g *GlockServer) grantLockOwnership(node *Node, owner, ownerID string, now time.Time) {
	lock := node.Lock
	lock.Owner = owner
	lock.OwnerID = ownerID
	lock.AcquiredAt = now
	lock.LastRefresh = now
	lock.Available = false
	lock.Token++
	lock.isHeld.Store(true)
	lock.recordAcquireAttempt(true)
	lock.recordOwnerChange(owner, ownerID, now, g.Config.OwnerHistoryMaxSize)

	node.IncrementAncestorCounts()
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

	if err := g.reserveLockSlot(); err != nil {
		return nil, http.StatusTooManyRequests, err
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
		g.releaseLockSlot()
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

		if node.WouldCreateCycle(newParentNode) {
			return nil, http.StatusBadRequest, fmt.Errorf("cannot set parent: would create a cycle in lock hierarchy")
		}

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
		g.releaseLockSlot()
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

	// Clear atomic flag if lock has expired
	// We hold the lock's mutex here, so this is safe
	if lock.isHeld.Load() && lock.IsAvailable(now) {
		lock.isHeld.Store(false)
	}

	// Check if lock is available (including TTL expiration check)
	// Also check hierarchy constraints - treat them like the lock being "unavailable"
	isAvailable := lock.IsAvailable(now) && !node.IsAnyParentHeld(now) && !node.HasHeldDescendants()

	if isAvailable {
		if lock.queue != nil && lock.queue.Size() > 0 {
			// Don't grant directly - the request should queue and wait its turn
		} else {
			// No queue or empty queue - grant immediately
			g.grantLockOwnership(node, req.Owner, req.OwnerID, now)
			lock.QueueSize = lock.getCurrentQueueSize()
			return lock, http.StatusOK, nil
		}
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

	queueResp := &QueueResponse{
		RequestID: requestID,
		Position:  position,
	}

	lock.QueueSize = lock.getCurrentQueueSize()
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

	// Try to grant lock from queue if available and hierarchy constraints are satisfied
	nodeTyped := node.(*Node)
	if lock.IsAvailable(now) && !nodeTyped.IsAnyParentHeld(now) && !nodeTyped.HasHeldDescendants() && lock.queue != nil {
		nextReq := lock.queue.PeekNext()
		if nextReq != nil && nextReq.ID == req.RequestID && nextReq.OwnerID == req.OwnerID {
			// Check if the request has expired
			if now.After(nextReq.TimeoutAt) {
				lock.queue.Dequeue() // Remove expired request
				return &PollResponse{Status: "expired"}, http.StatusGone, nil
			}

			// Grant the lock to this request
			lock.queue.Dequeue()
			g.grantLockOwnership(nodeTyped, nextReq.Owner, nextReq.OwnerID, now)
			lock.QueueSize = lock.getCurrentQueueSize()

			return &PollResponse{
				Status: "ready",
				Lock:   lock,
			}, http.StatusOK, nil
		}
	}

	if lock.queue != nil && lock.queue.Contains(req.RequestID) {
		position := lock.queue.GetPosition(req.RequestID)

		var queueReq *QueueRequest
		for _, r := range lock.queue.GetAll() {
			if r.ID == req.RequestID {
				queueReq = r
				break
			}
		}

		if queueReq != nil && now.After(queueReq.TimeoutAt) {
			lock.queue.Remove(req.RequestID)
			return &PollResponse{Status: "expired"}, http.StatusGone, nil
		}

		return &PollResponse{
			Status:   "waiting",
			Position: position,
		}, http.StatusOK, nil
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

	requests := lock.queue.GetAll()

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

// checkAndGrantFromQueue checks if a lock is available (including TTL expiration and hierarchy)
// and automatically grants it to the next queued request if one exists.
// The caller must hold lock.mu.
// Returns true if the lock was granted from the queue.
func (g *GlockServer) checkAndGrantFromQueue(node *Node, now time.Time) bool {
	lock := node.Lock
	if !lock.IsAvailable(now) {
		return false
	}

	// Check hierarchy constraints
	if node.IsAnyParentHeld(now) || node.HasHeldDescendants() {
		return false
	}

	// Lock is available and hierarchy is clear, try to grant it from the queue
	if g.tryGrantFromQueue(node, now) {
		lock.QueueSize = lock.getCurrentQueueSize()
		return true
	}

	return false
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

	lock.recordRelease()

	// Decrement ancestor counts since this lock is being released
	node.DecrementAncestorCounts()

	lock.Owner = ""
	lock.OwnerID = ""
	lock.AcquiredAt = time.Time{}
	lock.LastRefresh = time.Time{}
	lock.Available = true
	lock.isHeld.Store(false) // Clear atomic flag for race-free hierarchy checks

	// Try to grant from this lock's queue
	g.tryGrantFromQueue(node, now)

	// Also try to grant from children and parent queues since hierarchy constraints may have cleared
	g.tryGrantFromRelatedQueues(node, now)

	lock.QueueSize = lock.getCurrentQueueSize()
	return true, http.StatusOK, nil
}
