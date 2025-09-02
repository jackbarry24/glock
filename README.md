## GLock (gee - lock)
### In-memory lock queue

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

  // Queue usage - Manual polling
  err := g.CreateLock("queued-lock", "30s", "5m", glock.QueueFIFO, "2m")
  lock, queueResp, err := g.AcquireOrQueue("queued-lock", "worker-1")
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

  // OR use the convenient AcquireOrWait method
  lock, err := g.AcquireOrWait("queued-lock", "worker-1", 30*time.Second)
  if lock != nil {
    lock.StartHeartbeat()
    // work ...
    _ = lock.Release()
  }
  ```
