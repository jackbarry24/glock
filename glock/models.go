package glock

import "time"

type QueueType string

const (
	QueueNone QueueType = "none"
	QueueFIFO QueueType = "fifo"
	QueueLIFO QueueType = "lifo"
)

type LockConfig struct {
	MaxTTL         string    `json:"max_ttl"`
	DefaultTTL     string    `json:"default_lease_ttl"`
	MaxQueue       int       `json:"queue_capacity"`
	QueueType      QueueType `json:"queue_type"`
	DefaultTimeout string    `json:"queue_timeout,omitempty"`
}

type CreateLockRequest struct {
	LockID string     `json:"lock_id"`
	Config LockConfig `json:"config"`
	Tags   []string   `json:"tags,omitempty"`
}

type AcquireLeaseRequest struct {
	ClientID string `json:"client_id"`
	LeaseTTL string `json:"lease_ttl,omitempty"`
	Wait     bool   `json:"wait"`
	Timeout  string `json:"timeout,omitempty"`
}

type AcquireResp struct {
	State           string    `json:"state"`
	LockID          string    `json:"lock_id"`
	LeaseID         string    `json:"lease_id,omitempty"`
	OwnerClientID   string    `json:"owner_client_id,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	FenceToken      int64     `json:"fence_token,omitempty"`
	WaitID          string    `json:"wait_id,omitempty"`
	Position        int       `json:"position,omitempty"`
	EstimatedWaitMs int64     `json:"estimated_wait_ms,omitempty"`
}

type RefreshLeaseRequest struct {
	ClientID string `json:"client_id"`
	LeaseID  string `json:"lease_id"`
	NewTTL   string `json:"new_ttl,omitempty"`
}

type RefreshResp struct {
	State      string    `json:"state"`
	LockID     string    `json:"lock_id"`
	LeaseID    string    `json:"lease_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	FenceToken int64     `json:"fence_token"`
}

type ReleaseLeaseRequest struct {
	ClientID string `json:"client_id"`
	LeaseID  string `json:"lease_id"`
}

type ReleaseResp struct {
	State             string `json:"state"`
	LockID            string `json:"lock_id"`
	NextOwnerClientID string `json:"next_owner_client_id,omitempty"`
}

type Lease struct {
	LockID     string
	LeaseID    string
	ExpiresAt  time.Time
	FenceToken int64
}

type QueueInfo struct {
	WaitID          string
	Position        int
	EstimatedWaitMs int64
}

type WaitEntryView struct {
	WaitID    string    `json:"wait_id"`
	ClientID  string    `json:"client_id"`
	Position  int       `json:"position"`
	TimeoutAt time.Time `json:"timeout_at"`
}

type LeaseDetail struct {
	LeaseID    string    `json:"lease_id"`
	ClientID   string    `json:"client_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	FenceToken int64     `json:"fence_token"`
}

type LockStatus struct {
	LockID   string       `json:"lock_id"`
	Owner    *LeaseDetail `json:"owner,omitempty"`
	QueueLen int          `json:"queue_len"`
	Config   any          `json:"config"`
	Tags     []string     `json:"tags,omitempty"`
}

type StatusResp struct {
	Status LockStatus `json:"status"`
}

type QueueGetResp struct {
	LockID string          `json:"lock_id"`
	Queue  []WaitEntryView `json:"queue"`
}

type MetricsResp struct {
	LockID  string `json:"lock_id"`
	Metrics any    `json:"metrics"`
}
