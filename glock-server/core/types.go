package core

import (
	"encoding/json"
	"fmt"
	"time"
)

type QueueType string

const (
	QueueTypeNone QueueType = "none"
	QueueTypeFIFO QueueType = "fifo"
	QueueTypeLIFO QueueType = "lifo"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return fmt.Errorf("invalid duration type")
	}
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Nanoseconds())
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

type LockConfig struct {
	MaxTTL         Duration  `json:"max_ttl" binding:"required"`
	DefaultTTL     Duration  `json:"default_ttl" binding:"required"`
	MaxQueue       int       `json:"max_queue"`
	QueueType      QueueType `json:"queue_type"`
	DefaultTimeout Duration  `json:"default_timeout" example:"5m"`
}

type ServerConfig struct {
	NumShards           int
	ExpiryTick          time.Duration
	Capacity            int
	DefaultMaxTTL       time.Duration
	DefaultTTL          time.Duration
	DefaultQueue        int
	DefaultQueueTimeout time.Duration
}

type Lease struct {
	LeaseID    string    `json:"lease_id"`
	ClientID   string    `json:"client_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	FenceToken int64     `json:"fence_token"`
}

type WaitEntryView struct {
	WaitID    string    `json:"wait_id"`
	ClientID  string    `json:"client_id"`
	Position  int       `json:"position"`
	TimeoutAt time.Time `json:"timeout_at"`
}

type LockStatus struct {
	LockID   string     `json:"lock_id"`
	Owner    *Lease     `json:"owner,omitempty"`
	QueueLen int        `json:"queue_len"`
	Config   LockConfig `json:"config"`
	Tags     []string   `json:"tags,omitempty"`
}

type ProcessStatus struct {
	UptimeSec    int64
	LocksTotal   int
	ActiveLeases int
	QueueTotal   int
	Shards       int
	Goroutines   int
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

type Shard struct {
	id       int
	cfg      ServerConfig
	clock    Clock
	msgCh    chan Msg
	stopCh   chan struct{}
	locks    map[string]*lockState
	idemKeys map[string]*idemEntry
}

type idemEntry struct {
	response  interface{}
	expiresAt time.Time
}

type lockState struct {
	cfg        LockConfig
	owner      *Lease
	queue      []queueEntry
	createdAt  time.Time
	metrics    *LockMetrics
	tags       []string
	fenceToken int64
}

type queueEntry struct {
	waitID    string
	clientID  string
	timeoutAt time.Time
	addedAt   time.Time
}

type LockMetrics struct {
	TotalAcquires     int64
	SuccessAcquires   int64
	FailedAcquires    int64
	TotalReleases     int64
	TotalRefreshes    int64
	TotalQueued       int64
	QueueTimeouts     int64
	OwnerChanges      int64
	CreatedAt         time.Time
	LastActivityAt    time.Time
	CurrentQueueLen   int
	AvgQueueTimeNs    int64
	CurrentHoldTimeNs int64
	AvgHoldTimeNs     int64
	MaxHoldTimeNs     int64
	TotalHoldTimeNs   int64
	UniqueOwners      int
	TTLExpirations    int64
	MaxTTLExpirations int64
	totalQueueTimeNs  int64
	queuePromotions   int64
	totalHoldCount    int64
	uniqueOwnerIDs    map[string]bool
}
