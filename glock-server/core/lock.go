package core

import (
	"sync"
	"sync/atomic"
	"time"
)

// Lock represents a distributed lock
// @Description Lock represents a distributed lock with metadata and timing information
type Lock struct {
	Name         string        `json:"name" example:"my-lock"`
	Parent       string        `json:"parent,omitempty" example:"/parent-lock"`
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
	metrics      *LockMetrics  `json:"-"`
	isHeld       atomic.Bool   `json:"-"`
}

// LockQueue is now defined in queue.go

func (l *Lock) IsAvailable(now time.Time) bool {
	if l.OwnerID == "" {
		return true
	}
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

func NewLockMetrics() *LockMetrics {
	now := time.Now()
	return &LockMetrics{
		CreatedAt:      now,
		LastActivityAt: now,
		OwnerHistory:   make([]OwnerRecord, 0),
	}
}

func (l *Lock) GetMetrics() *LockMetrics {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	l.metrics.CurrentQueueSize = int64(l.getCurrentQueueSize())
	if !l.Available && l.OwnerID != "" {
		l.metrics.CurrentHoldTime = time.Since(l.AcquiredAt)
	}

	return l.metrics
}

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

func (l *Lock) recordQueueRequest() {
	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	atomic.AddInt64(&l.metrics.TotalQueuedRequests, 1)
	atomic.AddInt64(&l.metrics.CurrentQueueSize, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) recordQueueTimeout() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.QueueTimeoutCount, 1)
	atomic.AddInt64(&l.metrics.CurrentQueueSize, -1)
}

func (l *Lock) recordOwnerChange(newOwner, newOwnerID string, acquiredAt time.Time, maxSize int) {
	if l.metrics == nil {
		l.metrics = NewLockMetrics()
	}

	atomic.AddInt64(&l.metrics.OwnerChangeCount, 1)

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

	if l.OwnerID != "" && len(l.metrics.OwnerHistory) > 0 {
		lastIdx := len(l.metrics.OwnerHistory) - 1
		if l.metrics.OwnerHistory[lastIdx].ReleasedAt == nil {
			releasedAt := time.Now()
			l.metrics.OwnerHistory[lastIdx].ReleasedAt = &releasedAt
			l.metrics.OwnerHistory[lastIdx].HoldTime = releasedAt.Sub(l.metrics.OwnerHistory[lastIdx].AcquiredAt)
		}
	}

	record := OwnerRecord{
		Owner:      newOwner,
		OwnerID:    newOwnerID,
		AcquiredAt: acquiredAt,
	}
	l.metrics.OwnerHistory = append(l.metrics.OwnerHistory, record)

	if len(l.metrics.OwnerHistory) > maxSize {
		l.metrics.OwnerHistory = l.metrics.OwnerHistory[len(l.metrics.OwnerHistory)-maxSize:]
	}
}

func (l *Lock) recordRelease() {
	if l.metrics == nil {
		return
	}

	holdTime := time.Since(l.AcquiredAt)
	atomic.AddInt64((*int64)(&l.metrics.TotalHoldTime), int64(holdTime))

	totalHolds := atomic.LoadInt64(&l.metrics.SuccessfulAcquires)
	if totalHolds > 0 {
		avgHoldTime := time.Duration(atomic.LoadInt64((*int64)(&l.metrics.TotalHoldTime)) / totalHolds)
		atomic.StoreInt64((*int64)(&l.metrics.AverageHoldTime), int64(avgHoldTime))
	}

	if holdTime > l.metrics.MaxHoldTime {
		l.metrics.MaxHoldTime = holdTime
	}

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

func (l *Lock) recordRefresh() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.RefreshCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) recordTTLExpiration() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.TTLExpirationCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) recordMaxTTLExpiration() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.MaxTTLExpirationCount, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) recordFailedOperation() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.FailedOperations, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) recordStaleToken() {
	if l.metrics == nil {
		return
	}

	atomic.AddInt64(&l.metrics.StaleTokenErrors, 1)
	atomic.AddInt64(&l.metrics.FailedOperations, 1)
	l.metrics.LastActivityAt = time.Now()
}

func (l *Lock) getCurrentQueueSize() int {
	if l.queue == nil {
		return 0
	}

	return l.queue.list.Len()
}
