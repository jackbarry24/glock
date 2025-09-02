## glock (Go client)

### Install
Use `replace` for local dev in this mono repo. For external use, tag the repo and import by module path.

### Usage
```go
g, err := glock.Connect("http://localhost:8080")
if err != nil { panic(err) }
l, err := g.Acquire("resource", "worker-1")
if err != nil { panic(err) }
l.StartHeartbeat()
// do work
_ = l.Release()
```

### API
- `Connect(serverURL string) (*Glock, error)`
- `(*Glock) Acquire(name, owner string) (*Lock, error)` - Immediate acquisition or fail
- `(*Glock) AcquireOrQueue(name, owner string) (*Lock, *QueueResponse, error)` - Acquire or queue
- `(*Glock) AcquireOrWait(name, owner string, timeout time.Duration) (*Lock, error)` - Acquire with timeout
- `(*Glock) PollQueue(name, requestID string) (*PollResponse, error)` - Check queue status
- `(*Glock) RemoveFromQueue(name, requestID string) error` - Remove queued request
- `(*Lock) StartHeartbeat()`
- `(*Lock) Release() error`

### Testing
`go test ./...`



