package glock

// QueueBehavior defines how lock queuing works
type QueueBehavior string

const (
	QueueNone QueueBehavior = "none"
	QueueFIFO QueueBehavior = "fifo"
	QueueLIFO QueueBehavior = "lifo"
)

type CreateRequest struct {
	Name         string        `json:"name" binding:"required"`
	TTL          string        `json:"ttl" binding:"required"`
	MaxTTL       string        `json:"max_ttl" binding:"required"`
	Metadata     any           `json:"metadata"`
	QueueType    QueueBehavior `json:"queue_type,omitempty"`
	QueueTimeout string        `json:"queue_timeout,omitempty"`
}

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

type AcquireRequest struct {
	Name    string `json:"name" binding:"required"`
	Owner   string `json:"owner" binding:"required"`
	OwnerID string `json:"owner_id" binding:"required"`
}

type AcquireResponse struct {
	Lock *Lock `json:"lock"`
}

type RefreshRequest struct {
	Name    string `json:"name" binding:"required"`
	OwnerID string `json:"owner_id" binding:"required"`
	Token   uint   `json:"token" binding:"required"`
}

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

// QueueResponse is returned when a lock is queued
type QueueResponse struct {
	RequestID string `json:"request_id"`
	Position  int    `json:"position"`
}

// PollResponse contains the current queue status
type PollResponse struct {
	Status   string `json:"status"` // "waiting", "ready", "expired", "not_found"
	Position int    `json:"position,omitempty"`
	Lock     *Lock  `json:"lock,omitempty"`
}
