## glock (Go client)

### Install
Use `replace` for local dev in this mono repo. For external use, tag the repo and import by module path.

### Usage
```go
g, err := glock.Connect("http://localhost:8080")
if err != nil { panic(err) }

// Create a lock
_, err = g.CreateLock("resource", glock.LockConfig{DefaultTTL: "30s", MaxTTL: "5m", QueueType: glock.QueueFIFO}, nil, "")
if err != nil { panic(err) }

// Acquire (non-waiting)
res, err := g.Acquire("resource", glock.AcquireOptions{LeaseTTL: "30s", Wait: false})
if err != nil { panic(err) }
if res.State != "granted" { panic("busy") }

// Or acquire with waiting helper
lease, err := g.AcquireOrWait("resource", "30s", 30*time.Second, 100*time.Millisecond)
if err != nil { panic(err) }

// Refresh and release
_, _ = g.Refresh("resource", lease.LeaseID, "30s", "")
_, _ = g.Release("resource", lease.LeaseID, "")
```

### API
- `Connect(serverURL string) (*Glock, error)`
- `ConnectWithConfig(config *ClientConfig) (*Glock, error)`
- `(*Glock) CreateLock(lockID string, cfg LockConfig, tags []string, idemKey string) (created bool, err error)`
- `(*Glock) Acquire(lockID string, opts AcquireOptions) (*AcquireResult, error)`
- `(*Glock) AcquireOrWait(lockID, leaseTTL string, timeout, pollInterval time.Duration) (*Lease, error)`
- `(*Glock) Refresh(lockID, leaseID, newTTL, idemKey string) (*RefreshResp, error)`
- `(*Glock) Release(lockID, leaseID, idemKey string) (*ReleaseResp, error)`
- `(*Glock) Status(lockID string) (*StatusResp, error)`
- `(*Glock) ListLocks() ([]string, error)`
- `(*Glock) DeleteLock(lockID string) error`
- `(*Glock) QueueGet(lockID string, limit int) ([]WaitEntryView, error)`
- `(*Glock) QueueCancel(lockID, waitID string) error`

### Testing
`go test ./...`



