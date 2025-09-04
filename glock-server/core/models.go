package core

import (
	"time"
)

// QueueBehavior defines how lock queuing works
// @Description QueueBehavior defines how lock queuing works
type QueueBehavior string

const (
	QueueNone QueueBehavior = "none"
	QueueFIFO QueueBehavior = "fifo"
	QueueLIFO QueueBehavior = "lifo"
)

// QueueRequest represents a queued lock acquisition request
type QueueRequest struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	OwnerID   string    `json:"owner_id"`
	QueuedAt  time.Time `json:"queued_at"`
	TimeoutAt time.Time `json:"timeout_at"`
}

// QueueResponse is returned when a lock is queued
type QueueResponse struct {
	RequestID string `json:"request_id"`
	Position  int    `json:"position"`
}

// PollRequest is used to check queue status
type PollRequest struct {
	RequestID string `json:"request_id" binding:"required"`
	OwnerID   string `json:"owner_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
}

// PollResponse contains the current queue status
type PollResponse struct {
	Status   string `json:"status"` // "waiting", "ready", "expired", "not_found"
	Position int    `json:"position,omitempty"`
	Lock     *Lock  `json:"lock,omitempty"`
}

// CreateRequest represents a request to create a new lock
// @Description CreateRequest represents a request to create a new lock
type CreateRequest struct {
	Name         string        `json:"name" binding:"required" example:"my-lock"`
	TTL          string        `json:"ttl" binding:"required" example:"30s"`
	MaxTTL       string        `json:"max_ttl" binding:"required" example:"5m"`
	Metadata     any           `json:"metadata"`
	QueueType    QueueBehavior `json:"queue_type,omitempty" example:"fifo"`
	QueueTimeout string        `json:"queue_timeout,omitempty" example:"1m"`
}

// CreateResponse represents the response from creating a lock
// @Description CreateResponse represents the response from creating a lock
type CreateResponse struct {
	Lock *Lock `json:"lock"`
}

type UpdateRequest struct {
	Name         string        `json:"name" binding:"required"`
	TTL          string        `json:"ttl" binding:"required"`
	MaxTTL       string        `json:"max_ttl" binding:"required"`
	Metadata     any           `json:"metadata"`
	QueueType    QueueBehavior `json:"queue_type,omitempty"`
	QueueTimeout string        `json:"queue_timeout,omitempty"`
}

type UpdateResponse struct {
	Lock *Lock `json:"lock"`
}

// AcquireRequest represents a request to acquire a lock
// @Description AcquireRequest represents a request to acquire a lock
type AcquireRequest struct {
	Name         string `json:"name" binding:"required" example:"my-lock"`
	Owner        string `json:"owner" binding:"required" example:"client-app"`
	OwnerID      string `json:"owner_id" binding:"required" example:"uuid-123"`
	QueueRequest *bool  `json:"queue_request,omitempty"` // nil = true (default), false = don't queue
}

// AcquireResponse represents the response from acquiring a lock
// @Description AcquireResponse represents the response from acquiring a lock
type AcquireResponse struct {
	Lock *Lock `json:"lock"`
}

// RefreshRequest represents a request to refresh a lock
// @Description RefreshRequest represents a request to refresh a lock
type RefreshRequest struct {
	Name    string `json:"name" binding:"required" example:"my-lock"`
	OwnerID string `json:"owner_id" binding:"required" example:"uuid-123"`
	Token   uint   `json:"token" binding:"required" example:"12345"`
}

// RefreshResponse represents the response from refreshing a lock
// @Description RefreshResponse represents the response from refreshing a lock
type RefreshResponse struct {
	Lock *Lock `json:"lock"`
}

type VerifyRequest struct {
	Name    string `json:"name" binding:"required"`
	OwnerID string `json:"owner_id" binding:"required"`
	Token   uint   `json:"token" binding:"required"`
}

type VerifyResponse struct {
	Success bool `json:"success" binding:"required"`
}

type ReleaseRequest struct {
	Name    string `json:"name" binding:"required"`
	OwnerID string `json:"owner_id" binding:"required"`
	Token   uint   `json:"token" binding:"required"`
}

type ReleaseResponse struct {
	Success bool `json:"success"`
}

// LockMetrics represents comprehensive metrics for a distributed lock
type LockMetrics struct {
	// Usage Statistics
	TotalAcquireAttempts int64         `json:"total_acquire_attempts"`
	SuccessfulAcquires   int64         `json:"successful_acquires"`
	FailedAcquires       int64         `json:"failed_acquires"`
	CurrentHoldTime      time.Duration `json:"current_hold_time"`
	TotalHoldTime        time.Duration `json:"total_hold_time"`
	AverageHoldTime      time.Duration `json:"average_hold_time"`
	MaxHoldTime          time.Duration `json:"max_hold_time"`

	// Queue Statistics
	CurrentQueueSize     int64         `json:"current_queue_size"`
	TotalQueuedRequests  int64         `json:"total_queued_requests"`
	AverageQueueWaitTime time.Duration `json:"average_queue_wait_time"`
	MaxQueueWaitTime     time.Duration `json:"max_queue_wait_time"`
	QueueTimeoutCount    int64         `json:"queue_timeout_count"`

	// TTL/Expiration Statistics
	TTLExpirationCount    int64 `json:"ttl_expiration_count"`
	MaxTTLExpirationCount int64 `json:"max_ttl_expiration_count"`
	RefreshCount          int64 `json:"refresh_count"`
	HeartbeatCount        int64 `json:"heartbeat_count"`

	// Owner Statistics
	OwnerChangeCount  int64 `json:"owner_change_count"`
	UniqueOwnersCount int64 `json:"unique_owners_count"`

	// Error Statistics
	FailedOperations int64 `json:"failed_operations"`
	StaleTokenErrors int64 `json:"stale_token_errors"`
	NetworkErrors    int64 `json:"network_errors"`

	// Time-based metrics
	CreatedAt      time.Time `json:"created_at"`
	LastActivityAt time.Time `json:"last_activity_at"`

	// Owner history (keep last N owners for trending)
	OwnerHistory []OwnerRecord `json:"owner_history,omitempty"`
}

// OwnerRecord tracks ownership changes
type OwnerRecord struct {
	Owner      string        `json:"owner"`
	OwnerID    string        `json:"owner_id"`
	AcquiredAt time.Time     `json:"acquired_at"`
	ReleasedAt *time.Time    `json:"released_at,omitempty"`
	HoldTime   time.Duration `json:"hold_time"`
}

// MetricsResponse represents the response containing lock metrics
type MetricsResponse struct {
	LockName string       `json:"lock_name"`
	Metrics  *LockMetrics `json:"metrics"`
}
