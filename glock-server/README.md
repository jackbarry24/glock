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

#### Runtime Configuration
- GET `/api/config` - Get current configuration
- POST `/api/config/update` - Update configuration at runtime

### Endpoints
- POST `/api/create` { name, ttl?, max_ttl?, metadata, queue_type?, queue_timeout? }
- POST `/api/update` { name, ttl?, max_ttl?, metadata, queue_type?, queue_timeout? }

**Duration Format**: All duration fields accept Go duration strings (e.g., "30s", "5m", "1h", "300ms")
- DELETE `/api/delete/:name`
- POST `/api/acquire` { name, owner, owner_id } → Returns lock or queue info
- POST `/api/refresh` { name, owner_id, token } → Extend lock expiration
- POST `/api/verify` { name, owner_id, token } → Check ownership
- POST `/api/release` { name, owner_id, token }  → Release
- POST `/api/poll` { name, request_id, owner_id } → Check queue status
- POST `/api/freeze/:name` → Freeze a lock to prevent acquisition/refresh
- POST `/api/unfreeze/:name` → Unfreeze a lock to allow acquisition/refresh
- GET `/api/status` → See all locks and ownership
- GET `/api/list` → See all locks

### Queue Functionality

Locks can be configured with queue behavior:
- `queue_type`: `"none"` (default), `"fifo"`, or `"lifo"`
- `queue_timeout`: Duration string after which queued requests expire (default: "5m")

When a lock is unavailable:
- `none`: Returns error immediately
- `fifo`/`lifo`: Queues the request and returns `{ "queue": { "request_id": "...", "position": 1 } }`

Example create request:
```json
POST /api/create
{
  "name": "my-lock",
  "ttl": "30s",
  "max_ttl": "5m",
  "queue_type": "fifo",
  "queue_timeout": "2m"
}
```

Clients can poll their queue status using:
```json
POST /api/poll
{
  "name": "lock-name",
  "request_id": "returned-from-acquire",
  "owner_id": "client-uuid"
}
```

Response:
```json
{
  "status": "waiting|ready|expired|not_found",
  "position": 2,  // if waiting
  "lock": {...}   // if ready
}
```

### Notes
- TTL, MaxTTL, and QueueTimeout accept Go duration strings (e.g., "30s", "5m", "1h", "300ms")
- `owner_id` is expected to be a UUID
- Queue timeouts automatically clean up expired requests
- All configuration values can be changed at runtime via `/config/update`
