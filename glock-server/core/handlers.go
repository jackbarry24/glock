package core

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateHandler handles the /create route.
// @Summary Create a new lock
// @Description Create a new distributed lock with optional queue configuration
// @Tags locks
// @Accept json
// @Produce json
// @Param request body CreateRequest true "Lock creation parameters"
// @Success 200 {object} CreateResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /create [post]
func CreateHandler(c *gin.Context, g *GlockServer) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	ttlDuration, err := time.ParseDuration(req.TTL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ttl format: %v", err)})
		return
	}
	if ttlDuration <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ttl must be greater than zero"})
		return
	}

	maxTTLDuration, err := time.ParseDuration(req.MaxTTL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid max_ttl format: %v", err)})
		return
	}
	if maxTTLDuration <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_ttl must be greater than zero"})
		return
	}

	if maxTTLDuration < ttlDuration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_ttl must be greater than or equal to ttl"})
		return
	}

	var queueTimeoutDuration time.Duration
	if req.QueueTimeout != "" {
		queueTimeoutDuration, err = time.ParseDuration(req.QueueTimeout)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid queue_timeout format: %v", err)})
			return
		}
		if queueTimeoutDuration <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "queue_timeout must be greater than zero"})
			return
		}
	}

	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "queue_type must be one of: none, fifo, lifo"})
			return
		}
	}

	lock, code, err := g.CreateLock(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"lock": lock})
}

// UpdateHandler handles the /update route.
// @Summary Update lock configuration
// @Description Update an existing lock's TTL, MaxTTL, and queue configuration
// @Tags locks
// @Accept json
// @Produce json
// @Param request body UpdateRequest true "Lock update parameters"
// @Success 200 {object} UpdateResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /update [post]
func UpdateHandler(c *gin.Context, g *GlockServer) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if req.TTL != "" {
		_, err := time.ParseDuration(req.TTL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ttl format: %v", err)})
			return
		}
	}

	if req.MaxTTL != "" {
		_, err := time.ParseDuration(req.MaxTTL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid max_ttl format: %v", err)})
			return
		}
	}

	if req.QueueTimeout != "" {
		_, err := time.ParseDuration(req.QueueTimeout)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid queue_timeout format: %v", err)})
			return
		}
	}

	if req.QueueType != "" {
		switch req.QueueType {
		case QueueNone, QueueFIFO, QueueLIFO:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "queue_type must be one of: none, fifo, lifo"})
			return
		}
	}

	lock, code, err := g.UpdateLock(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"lock": lock})
}

// DeleteHandler handles the /delete/:name route.
// @Summary Delete a lock
// @Description Delete an existing lock by name
// @Tags locks
// @Accept json
// @Produce json
// @Param name path string true "Lock name"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /delete/{name} [delete]
func DeleteHandler(c *gin.Context, g *GlockServer) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	success, code, err := g.DeleteLock(name)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"success": success})
}

// AcquireHandler handles the /acquire route.
// @Summary Acquire a lock
// @Description Attempt to acquire a lock, either immediately or queue the request
// @Tags locks
// @Accept json
// @Produce json
// @Param request body AcquireRequest true "Lock acquisition parameters"
// @Success 200 {object} AcquireResponse
// @Success 202 {object} QueueResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /acquire [post]
func AcquireHandler(c *gin.Context, g *GlockServer) {
	var req AcquireRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if !IsValidOwner(req.OwnerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner_id must be valid UUID"})
		return
	}

	result, code, err := g.AcquireLock(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	switch response := result.(type) {
	case *Lock:
		c.JSON(code, gin.H{"lock": response})
	case *QueueResponse:
		c.JSON(code, gin.H{"queue": response})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unexpected response type"})
	}
}

// RefreshHandler handles the /refresh route.
// @Summary Refresh a lock
// @Description Extend the TTL of an acquired lock
// @Tags locks
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Lock refresh parameters"
// @Success 200 {object} RefreshResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /refresh [post]
func RefreshHandler(c *gin.Context, g *GlockServer) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if !IsValidOwner(req.OwnerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner_id must be valid UUID"})
		return
	}

	if req.Token == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fencing token required for refresh requests"})
	}

	lock, code, err := g.RefreshLock(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"lock": lock})
}

// VerifyHandler handles the /verify route.
// @Summary Verify lock ownership
// @Description Verify that a client still owns a lock
// @Tags locks
// @Accept json
// @Produce json
// @Param request body VerifyRequest true "Lock verification parameters"
// @Success 200 {object} VerifyResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /verify [post]
func VerifyHandler(c *gin.Context, g *GlockServer) {
	var req VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if req.Token == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fencing token required for verify requests"})
	}

	ok, code, err := g.VerifyLock(&req)
	if err != nil || !ok {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"success": ok})
}

// ReleaseHandler handles the /release route.
// @Summary Release a lock
// @Description Release ownership of a lock
// @Tags locks
// @Accept json
// @Produce json
// @Param request body ReleaseRequest true "Lock release parameters"
// @Success 200 {object} ReleaseResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /release [post]
func ReleaseHandler(c *gin.Context, g *GlockServer) {
	var req ReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if !IsValidOwner(req.OwnerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner_id must be valid UUID"})
		return
	}

	if req.Token == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fencing token required for release requests"})
	}

	success, code, err := g.ReleaseLock(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"success": success})
}

// PollHandler handles the /poll route for checking queue status
// @Summary Poll queue status
// @Description Check the status of a queued lock acquisition request
// @Tags queue
// @Accept json
// @Produce json
// @Param request body PollRequest true "Queue poll parameters"
// @Success 200 {object} PollResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /poll [post]
func PollHandler(c *gin.Context, g *GlockServer) {
	var req PollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if req.RequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id must not be empty"})
		return
	}

	if !IsValidOwner(req.OwnerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner_id must be valid UUID"})
		return
	}

	resp, code, err := g.PollQueue(&req)
	if err != nil && resp == nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, resp)
}

// RemoveFromQueueHandler handles the /remove route for removing queued requests
// @Summary Remove a request from queue
// @Description Remove a queued lock acquisition request
// @Tags queue
// @Accept json
// @Produce json
// @Param request body PollRequest true "Queue removal parameters"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /remove [post]
func RemoveFromQueueHandler(c *gin.Context, g *GlockServer) {
	var req PollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	if req.RequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id must not be empty"})
		return
	}

	if !IsValidOwner(req.OwnerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner_id must be valid UUID"})
		return
	}

	success, code, err := g.RemoveFromQueue(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, gin.H{"removed": success})
}

// ListQueueHandler handles the /list route for listing all queued requests
// @Summary List all requests in queue
// @Description Get a list of all queued lock acquisition requests for a specific lock
// @Tags queue
// @Accept json
// @Produce json
// @Param request body QueueListRequest true "Queue list parameters"
// @Success 200 {object} QueueListResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /list [post]
func ListQueueHandler(c *gin.Context, g *GlockServer) {
	var req QueueListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	response, code, err := g.ListQueue(&req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(code, response)
}

// StatusHandler handles the /status route.
// @Summary Get server status
// @Description Get the current status of all locks on the server
// @Tags status
// @Accept json
// @Produce json
// @Success 200 {object} map[string][]Lock
// @Router /status [get]
func StatusHandler(c *gin.Context, g *GlockServer) {
	locks := make([]*Lock, 0)
	g.Locks.Range(func(key, value any) bool {
		if node, ok := value.(*Node); ok {
			lock := node.Lock
			lock.mu.Lock()
			lock.QueueSize = lock.getCurrentQueueSize()
			lock.mu.Unlock()
			locks = append(locks, lock)
		}
		return true
	})
	c.JSON(http.StatusOK, gin.H{"locks": locks})
}

// ListHandler handles the /list route.
// @Summary List all locks
// @Description Get a list of all lock names on the server
// @Tags status
// @Accept json
// @Produce json
// @Success 200 {object} map[string][]string
// @Router /list [get]
func ListHandler(c *gin.Context, g *GlockServer) {
	locks := make([]*string, 0)
	g.Locks.Range(func(key, value any) bool {
		if node, ok := value.(*Node); ok {
			locks = append(locks, &node.Lock.Name)
		}
		return true
	})
	c.JSON(http.StatusOK, gin.H{"locks": locks})
}

// ConfigHandler handles the /config route to get current configuration
// @Summary Get server configuration
// @Description Get the current server configuration
// @Tags config
// @Accept json
// @Produce json
// @Success 200 {object} map[string]Config
// @Router /config [get]
func ConfigHandler(c *gin.Context, g *GlockServer) {
	c.JSON(http.StatusOK, gin.H{"config": g.Config})
}

// UpdateConfigHandler handles the /config/update route for runtime configuration updates
// @Summary Update server configuration
// @Description Update the server configuration at runtime
// @Tags config
// @Accept json
// @Produce json
// @Param request body Config true "New configuration"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /config/update [post]
func UpdateConfigHandler(c *gin.Context, g *GlockServer) {
	var req Config
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	g.Config.Capacity = req.Capacity
	g.Config.DefaultTTL = req.DefaultTTL
	g.Config.DefaultMaxTTL = req.DefaultMaxTTL
	g.Config.DefaultQueueTimeout = req.DefaultQueueTimeout
	g.Config.CleanupInterval = req.CleanupInterval

	c.JSON(http.StatusOK, gin.H{"message": "configuration updated", "config": g.Config})
}

// FreezeLockHandler handles the /freeze route
// @Summary Freeze a lock
// @Description Freeze a lock to prevent acquisition and refresh
// @Tags locks
// @Accept json
// @Produce json
// @Param name path string true "Lock name"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /freeze/{name} [post]
func FreezeLockHandler(c *gin.Context, g *GlockServer) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	lockVal, exists := g.Locks.Load(name)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "lock not found"})
		return
	}

	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.Frozen {
		c.JSON(http.StatusOK, gin.H{"frozen": true, "message": "lock is already frozen"})
		return
	}

	lock.Frozen = true
	c.JSON(http.StatusOK, gin.H{"frozen": true, "message": "lock frozen successfully"})
}

// UnfreezeLockHandler handles the /unfreeze route
// @Summary Unfreeze a lock
// @Description Unfreeze a lock to allow acquisition and refresh
// @Tags locks
// @Accept json
// @Produce json
// @Param name path string true "Lock name"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /unfreeze/{name} [post]
func UnfreezeLockHandler(c *gin.Context, g *GlockServer) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be empty"})
		return
	}

	lockVal, exists := g.Locks.Load(name)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "lock not found"})
		return
	}

	lock := lockVal.(*Lock)
	lock.mu.Lock()
	defer lock.mu.Unlock()

	if !lock.Frozen {
		c.JSON(http.StatusOK, gin.H{"frozen": false, "message": "lock is already unfrozen"})
		return
	}

	lock.Frozen = false
	c.JSON(http.StatusOK, gin.H{"frozen": false, "message": "lock unfrozen successfully"})
}

// MetricsHandler handles the /metrics/:name route.
// @Summary Get lock metrics
// @Description Get comprehensive metrics for a specific lock
// @Tags metrics
// @Accept json
// @Produce json
// @Param name path string true "Lock name"
// @Success 200 {object} MetricsResponse
// @Failure 404 {object} map[string]string
// @Router /metrics/{name} [get]
func MetricsHandler(c *gin.Context, g *GlockServer) {
	lockName := c.Param("name")
	if lockName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lock name is required"})
		return
	}

	lockVal, exists := g.Locks.Load(lockName)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "lock not found"})
		return
	}

	node := lockVal.(*Node)
	lock := node.Lock
	metrics := lock.GetMetrics()

	response := MetricsResponse{
		LockName: lockName,
		Metrics:  metrics,
	}

	c.JSON(http.StatusOK, response)
}
