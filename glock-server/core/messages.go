package core

import "time"

type Msg interface{ isMsg() }

type AcquireReq struct {
	LockID   string           `json:"lock_id" binding:"required"`
	ClientID string           `json:"client_id" binding:"required,uuid"`
	LeaseTTL Duration         `json:"lease_ttl"`
	Wait     bool             `json:"wait"`
	Timeout  Duration         `json:"timeout"`
	IdemKey  string           `json:"idempotency_key,omitempty"`
	Reply    chan AcquireResp `json:"-"`
}

func (AcquireReq) isMsg() {}

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
	Err             error     `json:"error,omitempty"`
}

type RefreshReq struct {
	LockID   string           `json:"lock_id" binding:"required"`
	ClientID string           `json:"client_id" binding:"required,uuid"`
	LeaseID  string           `json:"lease_id" binding:"required"`
	NewTTL   Duration         `json:"new_ttl"`
	IdemKey  string           `json:"idempotency_key,omitempty"`
	Reply    chan RefreshResp `json:"-"`
}

func (RefreshReq) isMsg() {}

type RefreshResp struct {
	State      string    `json:"state"`
	LockID     string    `json:"lock_id"`
	LeaseID    string    `json:"lease_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	FenceToken int64     `json:"fence_token"`
	Err        error     `json:"error,omitempty"`
}

type ReleaseReq struct {
	LockID   string           `json:"lock_id" binding:"required"`
	ClientID string           `json:"client_id" binding:"required,uuid"`
	LeaseID  string           `json:"lease_id" binding:"required"`
	IdemKey  string           `json:"idempotency_key,omitempty"`
	Reply    chan ReleaseResp `json:"-"`
}

func (ReleaseReq) isMsg() {}

type ReleaseResp struct {
	State             string `json:"state"`
	LockID            string `json:"lock_id"`
	NextOwnerClientID string `json:"next_owner_client_id,omitempty"`
	Err               error  `json:"error,omitempty"`
}

type StatusReq struct {
	LockID string          `json:"lock_id" binding:"required"`
	Reply  chan StatusResp `json:"-"`
}

func (StatusReq) isMsg() {}

type StatusResp struct {
	Status LockStatus `json:"status"`
	Err    error      `json:"error,omitempty"`
}

type QueueGetReq struct {
	LockID string            `json:"lock_id" binding:"required"`
	Limit  int               `json:"limit"`
	Reply  chan QueueGetResp `json:"-"`
}

func (QueueGetReq) isMsg() {}

type QueueGetResp struct {
	LockID string          `json:"lock_id"`
	Queue  []WaitEntryView `json:"queue"`
	Err    error           `json:"error,omitempty"`
}

type QueueDelReq struct {
	LockID   string            `json:"lock_id" binding:"required"`
	WaitID   string            `json:"wait_id" binding:"required"`
	ClientID string            `json:"client_id" binding:"required,uuid"`
	Reply    chan QueueDelResp `json:"-"`
}

func (QueueDelReq) isMsg() {}

type QueueDelResp struct {
	Removed bool  `json:"removed"`
	Err     error `json:"error,omitempty"`
}

type CreateReq struct {
	LockID   string          `json:"lock_id" binding:"required"`
	Config   LockConfig      `json:"config" binding:"required"`
	Tags     []string        `json:"tags,omitempty"`
	Metadata any             `json:"metadata,omitempty"`
	IdemKey  string          `json:"idempotency_key,omitempty"`
	Reply    chan CreateResp `json:"-"`
}

func (CreateReq) isMsg() {}

type CreateResp struct {
	LockID  string     `json:"lock_id"`
	Config  LockConfig `json:"config"`
	Created bool       `json:"created"`
	Err     error      `json:"error,omitempty"`
}

type DeleteReq struct {
	LockID string          `json:"lock_id" binding:"required"`
	Reply  chan DeleteResp `json:"-"`
}

func (DeleteReq) isMsg() {}

type DeleteResp struct {
	LockID  string `json:"lock_id"`
	Deleted bool   `json:"deleted"`
	Err     error  `json:"error,omitempty"`
}

type listReq struct {
	Reply chan []string
}

func (listReq) isMsg() {}

type ErrorResp struct {
	Error string `json:"error"`
}

type MetricsReq struct {
	LockID string           `json:"lock_id" binding:"required"`
	Reply  chan MetricsResp `json:"-"`
}

func (MetricsReq) isMsg() {}

type MetricsResp struct {
	LockID  string      `json:"lock_id"`
	Metrics LockMetrics `json:"metrics"`
	Err     error       `json:"error,omitempty"`
}

type AllStatusReq struct {
	Reply chan AllStatusResp `json:"-"`
}

func (AllStatusReq) isMsg() {}

type AllStatusResp struct {
	Statuses []LockStatus `json:"statuses"`
	Err      error        `json:"error,omitempty"`
}
