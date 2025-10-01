package core

import (
	"container/list"
	"time"
)

// LockQueue manages a queue of lock acquisition requests.
// It supports FIFO, LIFO ordering and tracks request positions.
type LockQueue struct {
	requests map[string]*list.Element
	list     *list.List
}

// NewLockQueue creates a new empty lock queue.
func NewLockQueue() *LockQueue {
	return &LockQueue{
		requests: make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Enqueue adds a new request to the queue.
// If isLIFO is true, the request is added to the front (LIFO behavior).
// Otherwise, it's added to the back (FIFO behavior).
func (q *LockQueue) Enqueue(req *QueueRequest, isLIFO bool) {
	element := q.list.PushBack(req)
	if isLIFO {
		q.list.MoveToFront(element)
	}
	q.requests[req.ID] = element
}

// Dequeue removes and returns the next request from the front of the queue.
// Returns nil if the queue is empty.
func (q *LockQueue) Dequeue() *QueueRequest {
	if q.list.Len() == 0 {
		return nil
	}

	element := q.list.Front()
	q.list.Remove(element)
	req := element.Value.(*QueueRequest)
	delete(q.requests, req.ID)
	return req
}

// Remove removes a specific request by ID from the queue.
// Returns the removed request or nil if not found.
func (q *LockQueue) Remove(requestID string) *QueueRequest {
	element, exists := q.requests[requestID]
	if !exists {
		return nil
	}

	delete(q.requests, requestID)
	q.list.Remove(element)
	return element.Value.(*QueueRequest)
}

// GetPosition returns the position of a request in the queue (1-indexed).
// Returns -1 if the request is not found.
func (q *LockQueue) GetPosition(requestID string) int {
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

// PeekNext returns the next request without removing it from the queue.
// Returns nil if the queue is empty.
func (q *LockQueue) PeekNext() *QueueRequest {
	if q.list.Len() == 0 {
		return nil
	}

	element := q.list.Front()
	return element.Value.(*QueueRequest)
}

// Size returns the number of requests currently in the queue.
func (q *LockQueue) Size() int {
	return q.list.Len()
}

// CleanExpired removes all expired requests from the queue.
// A request is considered expired if the current time is after its timeout plus a grace period.
// Returns the number of requests removed.
func (q *LockQueue) CleanExpired(now time.Time) int {
	removed := 0
	gracePeriod := 5 * time.Second

	for e := q.list.Front(); e != nil; {
		req := e.Value.(*QueueRequest)
		next := e.Next()

		if now.After(req.TimeoutAt.Add(gracePeriod)) {
			delete(q.requests, req.ID)
			q.list.Remove(e)
			removed++
		}
		e = next
	}
	return removed
}

// GetAll returns a slice of all requests currently in the queue.
// The requests are returned in queue order (front to back).
func (q *LockQueue) GetAll() []*QueueRequest {
	requests := make([]*QueueRequest, 0, q.list.Len())
	for e := q.list.Front(); e != nil; e = e.Next() {
		req := e.Value.(*QueueRequest)
		requests = append(requests, req)
	}
	return requests
}

// Contains checks if a request with the given ID exists in the queue.
func (q *LockQueue) Contains(requestID string) bool {
	_, exists := q.requests[requestID]
	return exists
}
