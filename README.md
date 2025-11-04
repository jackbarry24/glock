## GLock (gee - lock)
### In-memory lock queue

This repo contains:
- `glock-server`: HTTP lock service (in-memory)
- `glock`: Go client library

### Quick start
- Run server: `cd glock-server && go run ./cmd`
- Use client:
  ```go
  package main

  import (
    "fmt"
    "time"
    "glock/glock"
  )

  func main() {
    g, err := glock.Connect("http://localhost:8080")
    if err != nil { panic(err) }

    // Create a lock (once)
    _, err = g.CreateLock("my-lock", glock.LockConfig{DefaultTTL: "30s", MaxTTL: "5m", QueueType: glock.QueueFIFO}, nil, "")
    if err != nil { panic(err) }

    // Acquire or wait until available
    lease, err := g.AcquireOrWait("my-lock", "30s", 30*time.Second, 100*time.Millisecond)
    if err != nil { panic(err) }
    fmt.Println("acquired", lease.LeaseID)

    // Optionally refresh before expiry
    _, _ = g.Refresh("my-lock", lease.LeaseID, "30s", "")

    // Release when done
    _, _ = g.Release("my-lock", lease.LeaseID, "")
  }
  ```
