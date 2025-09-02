## Glock - Distributed Locking (Mono repo)

This repo contains:
- `glock-server`: HTTP lock service (in-memory)
- `glock`: Go client library

### Quick start
- Run server: `cd glock-server && go run ./cmd`
- Use client:
  ```go
  g, _ := glock.Connect("http://localhost:8080")

  // Basic usage
  l, _ := g.Acquire("my-lock", "worker-1")
  l.StartHeartbeat()
  // work ...
  _ = l.Release()

  // Queue usage
  err := g.CreateLock("queued-lock", "30s", "5m", glock.QueueFIFO, "2m")
  lock, queueResp, err := g.AcquireQueued("queued-lock", "worker-1")
  if queueResp != nil {
    // Lock is queued, poll for status
    for {
      resp, _ := g.PollQueue("queued-lock", queueResp.RequestID)
      if resp.Status == "ready" {
        lock = resp.Lock
        break
      }
      time.Sleep(time.Second)
    }
  }
  if lock != nil {
    lock.StartHeartbeat()
    // work ...
    _ = lock.Release()
  }
  ```
