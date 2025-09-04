package core

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// Lock represents a distributed lock
// @Description Lock represents a distributed lock with metadata and timing information
type Lock struct {
	Name         string        `json:"name" example:"my-lock"`
	Owner        string        `json:"owner" example:"client-app"`
	OwnerID      string        `json:"owner_id" example:"uuid-123"`
	AcquiredAt   time.Time     `json:"acquired_at"`
	LastRefresh  time.Time     `json:"last_refresh"`
	Available    bool          `json:"available" example:"true"`
	Token        uint          `json:"token" example:"12345"`
	TTL          time.Duration `json:"ttl" swaggertype:"string" example:"30s"`
	MaxTTL       time.Duration `json:"max_ttl" swaggertype:"string" example:"5m"`
	Metadata     any           `json:"metadata"`
	QueueType    QueueBehavior `json:"queue_type" example:"fifo"`
	QueueTimeout time.Duration `json:"queue_timeout" swaggertype:"string" example:"1m"`
	QueueSize    int           `json:"queue_size" example:"0"`
	Frozen       bool          `json:"frozen" example:"false"`
	queue        *LockQueue    `json:"-"`
	mu           sync.Mutex    `json:"-"`
	metrics      *LockMetrics  `json:"-"` // Metrics tracking
}

// LockQueue manages queued lock acquisition requests
type LockQueue struct {
	requests map[string]*list.Element // requestID -> list element
	list     *list.List               // doubly linked list for FIFO/LIFO
	mu       sync.RWMutex             `json:"-"`
}

// NewLockQueue creates a new queue for lock requests
func NewLockQueue() *LockQueue {
	return &LockQueue{
		requests: make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Enqueue adds a request to the queue
func (q *LockQueue) Enqueue(req *QueueRequest, isLIFO bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	element := q.list.PushBack(req)
	if isLIFO {
		// For LIFO, move to front
		q.list.MoveToFront(element)
	}
	q.requests[req.ID] = element
}

// Dequeue removes and returns the next request
func (q *LockQueue) Dequeue() *QueueRequest {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.list.Len() == 0 {
		return nil
	}

	element := q.list.Front()
	q.list.Remove(element)
	req := element.Value.(*QueueRequest)
	delete(q.requests, req.ID)
	return req
}

// Remove removes a specific request by ID
func (q *LockQueue) Remove(requestID string) *QueueRequest {
	q.mu.Lock()
	defer q.mu.Unlock()

	element, exists := q.requests[requestID]
	if !exists {
		return nil
	}

	delete(q.requests, requestID)
	q.list.Remove(element)
	return element.Value.(*QueueRequest)
}

// GetPosition returns the position of a request in the queue (1-based)
func (q *LockQueue) GetPosition(requestID string) int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	element, exists := q.requests[requestID]
	if !exists {
		return -1
	}

	position := 1
	for e := q.list.Front(); e != nil; e = e.Next() {
		if e == element {
			return position
		}
		position++
	}
	return -1
}

// GetNext returns the next request without removing it
func (q *LockQueue) GetNext() *QueueRequest {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.list.Len() == 0 {
		return nil
	}

	element := q.list.Front()
	return element.Value.(*QueueRequest)
}

// Size returns the current queue size
func (q *LockQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.list.Len()
}

// CleanExpired removes expired requests and returns the count removed
// Only removes requests that have been expired for more than 5 seconds
// to allow polling to detect recently expired requests
func (q *LockQueue) CleanExpired(now time.Time) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	removed := 0
	gracePeriod := 5 * time.Second // Allow recently expired requests to be detected

	for e := q.list.Front(); e != nil; {
		req := e.Value.(*QueueRequest)
		next := e.Next()

		// Only remove if expired for more than the grace period
		if now.After(req.TimeoutAt.Add(gracePeriod)) {
			delete(q.requests, req.ID)
			q.list.Remove(e)
			removed++
		}
		e = next
	}
	return removed
}

func (l *Lock) IsAvailable() bool {
	if l.OwnerID == "" {
		return true
	}
	now := time.Now()
	if now.After(l.AcquiredAt.Add(l.MaxTTL)) {
		return true
	}
	if now.After(l.LastRefresh.Add(l.TTL)) {
		return true
	}
	return l.Available
}

// isExpired checks if the lock has expired but doesn't modify state
func (l *Lock) isExpired() bool {
	if l.OwnerID == "" {
		return false
	}
	now := time.Now()
	return now.After(l.AcquiredAt.Add(l.MaxTTL)) || now.After(l.LastRefresh.Add(l.TTL))
}

func (l *Lock) GetOwner() string {
	if l.Owner != "" {
		return l.Owner
	}
	if l.OwnerID != "" {
		return l.OwnerID
	}
	return "none"
}

// NewLockMetrics creates a new metrics instance for a lock
func NewLockMetrics() *LockMetrics {
	now := time.Now()
	return &LockMetrics{
		CreatedAt:      now,
		LastActivityAt: now,
		OwnerHistory:   make([]OwnerRecord, 0),
	}
}

// GetMetrics returns the lock's metrics (thread-safe)
func (l *Lock) GetMetrics() *LockMetrics {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	// Update current metrics
	l.metrics.CurrentQueueSize = int64(l.getCurrentQueueSize())
	if !l.Available && l.OwnerID != "" {
		l.metrics.CurrentHoldTime = time.Since(l.AcquiredAt)
	}

	return l.metrics
}

// recordAcquireAttempt records an acquire attempt
func (l *Lock) recordAcquireAttempt(success bool) {
	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	atomic.AddInt64(&l.metrics.TotalAcquireAttempts, 1)
	l.metrics.LastActivityAt = time.Now()

	if success {
		atomic.AddInt64(&l.metrics.SuccessfulAcquires, 1)
	} else {
		atomic.AddInt64(&l.metrics.FailedAcquires, 1)
	}
}

// recordQueueRequest records a queue request
func (l *Lock) recordQueueRequest() {
	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	atomic.AddInt64(&l.metrics.TotalQueuedRequests, 1)
	atomic.AddInt64(&l.metrics.CurrentQueueSize, 1)
	l.metrics.LastActivityAt = time.Now()
}

// recordQueueTimeout records a queue timeout
func (l *Lock) recordQueueTimeout() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.QueueTimeoutCount, 1)
	atomic.AddInt64(&l.metrics.CurrentQueueSize, -1)
}

// recordOwnerChange records when ownership changes
func (l *Lock) recordOwnerChange(newOwner, newOwnerID string, acquiredAt time.Time) {
	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	atomic.AddInt64(&l.metrics.OwnerChangeCount, 1)

	// Check if this is a new unique owner
	isUnique := true
	for _, record := range l.metrics.OwnerHistory {
		if record.OwnerID == newOwnerID {
			isUnique = false
			break
		}
	}
	if isUnique {
		atomic.AddInt64(&l.metrics.UniqueOwnersCount, 1)
	}

	// Record previous owner if lock was held
	if l.OwnerID != "" && len(l.metrics.OwnerHistory) > 0 {
		// Update the last record with release time
		lastIdx := len(l.metrics.OwnerHistory) - 1
		if l.metrics.OwnerHistory[lastIdx].ReleasedAt == nil {
			releasedAt := time.Now()
			l.metrics.OwnerHistory[lastIdx].ReleasedAt = &releasedAt
			l.metrics.OwnerHistory[lastIdx].HoldTime = releasedAt.Sub(l.metrics.OwnerHistory[lastIdx].AcquiredAt)
		}
	}

	// Add new owner record
	record := OwnerRecord{
		Owner:      newOwner,
		OwnerID:    newOwnerID,
		AcquiredAt: acquiredAt,
	}
	l.metrics.OwnerHistory = append(l.metrics.OwnerHistory, record)

	// Keep only last 20 records to prevent unbounded growth
	if len(l.metrics.OwnerHistory) > 20 {
		l.metrics.OwnerHistory = l.metrics.OwnerHistory[len(l.metrics.OwnerHistory)-20:]
	}
}

// recordRelease records a lock release
func (l *Lock) recordRelease() {
	if l.metrics == nil {
		return
	}

	// Update hold time statistics
	holdTime := time.Since(l.AcquiredAt)
	atomic.AddInt64((*int64)(&l.metrics.TotalHoldTime), int64(holdTime))

	// Update average hold time
	totalHolds := atomic.LoadInt64(&l.metrics.SuccessfulAcquires)
	if totalHolds > 0 {
		avgHoldTime := time.Duration(atomic.LoadInt64((*int64)(&l.metrics.TotalHoldTime)) / totalHolds)
		atomic.StoreInt64((*int64)(&l.metrics.AverageHoldTime), int64(avgHoldTime))
	}

	// Update max hold time
	if holdTime > l.metrics.MaxHoldTime {
		l.metrics.MaxHoldTime = holdTime
	}

	// Update last owner record
	if len(l.metrics.OwnerHistory) > 0 {
		lastIdx := len(l.metrics.OwnerHistory) - 1
		if l.metrics.OwnerHistory[lastIdx].ReleasedAt == nil {
			releasedAt := time.Now()
			l.metrics.OwnerHistory[lastIdx].ReleasedAt = &releasedAt
			l.metrics.OwnerHistory[lastIdx].HoldTime = releasedAt.Sub(l.metrics.OwnerHistory[lastIdx].AcquiredAt)
		}
	}

	l.metrics.LastActivityAt = time.Now()
}

// recordRefresh records a lock refresh
func (l *Lock) recordRefresh() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.RefreshCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

// recordHeartbeat records a heartbeat
func (l *Lock) recordHeartbeat() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.HeartbeatCount, 1)
}

// recordTTLExpiration records a TTL expiration
func (l *Lock) recordTTLExpiration() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.TTLExpirationCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

// recordMaxTTLExpiration records a Max TTL expiration
func (l *Lock) recordMaxTTLExpiration() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.MaxTTLExpirationCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

// recordFailedOperation records a failed operation
func (l *Lock) recordFailedOperation() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.FailedOperations, 1)
	l.metrics.LastActivityAt = time.Now()
}

// recordStaleToken records a stale token error
func (l *Lock) recordStaleToken() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.StaleTokenErrors, 1)
	atomic.AddInt64(&l.metrics.FailedOperations, 1)
	l.metrics.LastActivityAt = time.Now()
}

// getCurrentQueueSize returns the current queue size (thread-safe)
func (l *Lock) getCurrentQueueSize() int {
	if l.queue == nil {
		return 0
	}

	l.queue.mu.RLock()
	defer l.queue.mu.RUnlock()
	return l.queue.list.Len()
}
