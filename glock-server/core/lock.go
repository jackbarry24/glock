package core

import (
	"container/list"
	"sync"
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
	queue        *LockQueue    `json:"-"`
	mu           sync.Mutex    `json:"-"`
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

func (l *Lock) GetOwner() string {
	if l.Owner != "" {
		return l.Owner
	}
	if l.OwnerID != "" {
		return l.OwnerID
	}
	return "none"
}
