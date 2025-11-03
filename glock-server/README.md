## glock-server (HTTP lock service)

### Run
```
go run ./cmd
```
Default: listens on `:8080`.

### Configuration

The server supports configuration via environment variables, YAML config file, or runtime updates.

#### Environment Variables
- `GLOCK_PORT` - Server port (default: 8080)
- `GLOCK_HOST` - Server host (default: "")
- `GLOCK_CAPACITY` - Maximum number of locks (default: 1000)
- `GLOCK_DEFAULT_TTL` - Default TTL duration (default: 30s)
- `GLOCK_DEFAULT_MAX_TTL` - Default MaxTTL duration (default: 5m)
- `GLOCK_DEFAULT_QUEUE_TIMEOUT` - Default queue timeout (default: 5m)

- `GLOCK_CLEANUP_INTERVAL` - Lock cleanup interval (default: 30s)
- `GLOCK_CONFIG_FILE` - Path to YAML config file

#### YAML Configuration
Create a `config.yaml` file (see `config.example.yaml`):
```yaml
port: 8080
host: ""
capacity: 1000
default_ttl: 30s
default_max_ttl: 5m
default_queue_timeout: 5m
cleanup_interval: 30s
```

### Endpoints
Base path: `/api`

- POST `/locks`
  - Body: `{ "lock_id": string, "config": { "default_ttl": duration, "max_ttl": duration, "max_queue"?: int, "queue_type"?: "none"|"fifo"|"lifo", "default_timeout"?: duration }, "tags"?: []string }`
  - Headers: `Idempotency-Key` (optional)
  - 201 Created / 200 OK → `{ "lock_id": string, "config": { ... }, "created": bool }`

- GET `/locks` → `{ "locks": [string] }`
- GET `/locks/{lock_id}` → `{ "status": { lock_id, owner?, queue_len, config, tags? } }`
- DELETE `/locks/{lock_id}` → `{ "lock_id": string, "deleted": bool }`

- POST `/locks/{lock_id}/lease`
  - Body: `{ "client_id": uuid, "lease_ttl"?: duration, "wait": bool, "timeout"?: duration }`
  - Headers: `Idempotency-Key` (optional)
  - 200 granted → `{ state:"granted", lock_id, lease_id, expires_at, fence_token }`
  - 202 queued → `{ state:"queued", lock_id, wait_id, position, estimated_wait_ms }`
  - 409 busy → `{ state:"busy", lock_id, owner_client_id, expires_at }`

- PUT `/locks/{lock_id}/lease`
  - Body: `{ "client_id": uuid, "lease_id": string, "new_ttl"?: duration }`
  - 200 → `{ state:"granted", lock_id, lease_id, expires_at, fence_token }`

- DELETE `/locks/{lock_id}/lease`
  - Body: `{ "client_id": uuid, "lease_id": string }`
  - 200 → `{ state:"released", lock_id, next_owner_client_id? }`

- GET `/locks/{lock_id}/queue?limit=100` → `{ lock_id, queue: [ { wait_id, client_id, position, timeout_at } ] }`
- DELETE `/locks/{lock_id}/queue/{wait_id}` with body `{ "client_id": uuid }` → `{ removed: bool }`

- GET `/locks/{lock_id}/metrics` → `{ lock_id, metrics: { ... } }`

### Queue Functionality

Locks support queues:
- `queue_type`: `"none"` (default), `"fifo"`, or `"lifo"`
- `default_timeout`: server-level fallback for queue wait timeouts

When `wait=true` and the lock is held, the server returns 202 with `wait_id` and `position`. On release/expiry, the next waiter is promoted. Clients can poll `GET /locks/{lock_id}` until `status.owner.client_id` matches theirs, or cancel via DELETE queue.

Note: The Go client library (`glock`) provides enum constants: `glock.QueueNone`, `glock.QueueFIFO`, `glock.QueueLIFO` for type-safe queue configuration.

### Notes
- Durations accept Go duration strings (e.g., "30s", "5m", "1h", "300ms"). JSON may also encode them as integer nanoseconds.
- `client_id` is expected to be a UUID
- Queue timeouts automatically clean up expired entries
