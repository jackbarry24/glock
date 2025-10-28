package core

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func mapErrorToStatus(err error) int {
	switch {
	case errors.Is(err, ErrLockNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrLockExists):
		return http.StatusConflict
	case errors.Is(err, ErrLockBusy):
		return http.StatusConflict
	case errors.Is(err, ErrQueueFull):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrCapacityReached):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrNotOwner):
		return http.StatusForbidden
	case errors.Is(err, ErrInvalidLease):
		return http.StatusBadRequest
	case errors.Is(err, ErrWaitNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrNotImplemented):
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// CreateHandler godoc
// @Summary Create a new lock
// @Description Creates a new distributed lock with the specified configuration
// @Tags locks
// @Accept json
// @Produce json
// @Param lock body CreateLockRequest true "Lock configuration"
// @Param Idempotency-Key header string false "Idempotency key for safe retries"
// @Success 201 {object} CreateResp "Lock created successfully"
// @Failure 400 {object} ErrorResp "Invalid request"
// @Failure 409 {object} ErrorResp "Lock already exists"
// @Failure 503 {object} ErrorResp "Server capacity reached"
// @Router /locks [post]
func CreateHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateLockRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: err.Error()})
			return
		}

		idemKey := c.GetHeader("Idempotency-Key")
		resp, err := s.Create(req.LockID, req.Config, req.Tags, idemKey)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		if resp.Created {
			c.JSON(http.StatusCreated, resp)
		} else {
			c.JSON(http.StatusOK, resp)
		}
	}
}

// DeleteHandler godoc
// @Summary Delete a lock
// @Description Deletes an existing lock and releases all queued waiters
// @Tags locks
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Success 200 {object} DeleteResp "Lock deleted successfully"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id} [delete]
func DeleteHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		resp, err := s.Delete(lockID)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// AcquireHandler godoc
// @Summary Acquire a lock (create lease)
// @Description Attempts to acquire a lock for a client. Returns granted if successful, queued if waiting, or busy if lock is held and wait=false
// @Tags locks
// @Accept json
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Param request body AcquireLeaseRequest true "Acquire request"
// @Param Idempotency-Key header string false "Idempotency key for safe retries"
// @Success 200 {object} AcquireResp "Lock acquired (granted)"
// @Success 202 {object} AcquireResp "Added to queue (queued)"
// @Success 409 {object} AcquireResp "Lock is busy"
// @Failure 400 {object} ErrorResp "Invalid request"
// @Failure 503 {object} ErrorResp "Queue full or capacity reached"
// @Router /locks/{lock_id}/lease [post]
func AcquireHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		var req AcquireLeaseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: err.Error()})
			return
		}

		idemKey := c.GetHeader("Idempotency-Key")
		fullReq := AcquireReq{
			LockID:   lockID,
			ClientID: req.ClientID,
			LeaseTTL: req.LeaseTTL,
			Wait:     req.Wait,
			Timeout:  req.Timeout,
			IdemKey:  idemKey,
		}

		resp, err := s.Acquire(fullReq)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		statusCode := http.StatusOK
		if resp.State == "queued" {
			statusCode = http.StatusAccepted
		} else if resp.State == "busy" {
			statusCode = http.StatusConflict
		}

		c.JSON(statusCode, resp)
	}
}

// RefreshHandler godoc
// @Summary Refresh a lock lease
// @Description Extends the TTL of an existing lock lease
// @Tags locks
// @Accept json
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Param request body RefreshLeaseRequest true "Refresh request"
// @Param Idempotency-Key header string false "Idempotency key for safe retries"
// @Success 200 {object} RefreshResp "Lease refreshed successfully"
// @Failure 400 {object} ErrorResp "Invalid request or lease"
// @Failure 403 {object} ErrorResp "Not the lock owner"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id}/lease [put]
func RefreshHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		var req RefreshLeaseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: err.Error()})
			return
		}

		idemKey := c.GetHeader("Idempotency-Key")
		fullReq := RefreshReq{
			LockID:   lockID,
			ClientID: req.ClientID,
			LeaseID:  req.LeaseID,
			NewTTL:   req.NewTTL,
			IdemKey:  idemKey,
		}

		resp, err := s.Refresh(fullReq)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// ReleaseHandler godoc
// @Summary Release a lock (delete lease)
// @Description Releases a lock held by a client, allowing the next queued client to acquire it
// @Tags locks
// @Accept json
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Param request body ReleaseLeaseRequest true "Release request"
// @Param Idempotency-Key header string false "Idempotency key for safe retries"
// @Success 200 {object} ReleaseResp "Lock released successfully"
// @Failure 400 {object} ErrorResp "Invalid request or lease"
// @Failure 403 {object} ErrorResp "Not the lock owner"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id}/lease [delete]
func ReleaseHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		var req ReleaseLeaseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: err.Error()})
			return
		}

		idemKey := c.GetHeader("Idempotency-Key")
		fullReq := ReleaseReq{
			LockID:   lockID,
			ClientID: req.ClientID,
			LeaseID:  req.LeaseID,
			IdemKey:  idemKey,
		}

		resp, err := s.Release(fullReq)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// StatusHandler godoc
// @Summary Get lock status
// @Description Returns the current status of a specific lock including owner and queue information
// @Tags locks
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Success 200 {object} StatusResp "Lock status"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id} [get]
func StatusHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		resp, err := s.Status(lockID)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// QueueGetHandler godoc
// @Summary Get lock queue
// @Description Returns the queue of clients waiting to acquire a lock
// @Tags locks
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Param limit query int false "Maximum number of queue entries to return" default(100)
// @Success 200 {object} QueueGetResp "Queue entries"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id}/queue [get]
func QueueGetHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		limit := 100
		if limitParam := c.Query("limit"); limitParam != "" {
			var parsed int
			if _, err := fmt.Sscanf(limitParam, "%d", &parsed); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		resp, err := s.QueueGet(lockID, limit)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// QueueDelHandler godoc
// @Summary Remove entry from lock queue
// @Description Removes a specific client's wait entry from a lock's queue
// @Tags locks
// @Accept json
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Param wait_id path string true "Wait ID"
// @Param request body QueueDeleteRequest true "Delete request"
// @Success 200 {object} QueueDelResp "Entry removed"
// @Failure 400 {object} ErrorResp "Invalid request"
// @Failure 404 {object} ErrorResp "Lock or wait entry not found"
// @Router /locks/{lock_id}/queue/{wait_id} [delete]
func QueueDelHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		waitID := c.Param("wait_id")
		if lockID == "" || waitID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id and wait_id are required"})
			return
		}

		var req QueueDeleteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: err.Error()})
			return
		}

		resp, err := s.QueueDel(lockID, waitID, req.ClientID)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// ListHandler godoc
// @Summary List all locks
// @Description Returns a list of all lock IDs in the system
// @Tags locks
// @Produce json
// @Success 200 {object} ListLocksResponse "List of lock IDs"
// @Failure 500 {object} ErrorResp "Internal server error"
// @Router /locks [get]
func ListHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		locks, err := s.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, ListLocksResponse{Locks: locks})
	}
}

// MetricsHandler godoc
// @Summary Get lock metrics
// @Description Returns detailed metrics and statistics for a specific lock
// @Tags locks
// @Produce json
// @Param lock_id path string true "Lock ID"
// @Success 200 {object} MetricsResp "Lock metrics"
// @Failure 404 {object} ErrorResp "Lock not found"
// @Router /locks/{lock_id}/metrics [get]
func MetricsHandler(s *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		lockID := c.Param("lock_id")
		if lockID == "" {
			c.JSON(http.StatusBadRequest, ErrorResp{Error: "lock_id is required"})
			return
		}

		resp, err := s.Metrics(lockID)
		if err != nil {
			c.JSON(mapErrorToStatus(err), ErrorResp{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

type CreateLockRequest struct {
	LockID string     `json:"lock_id" binding:"required" example:"my-resource-lock"`
	Config LockConfig `json:"config" binding:"required"`
	Tags   []string   `json:"tags,omitempty" example:"production,critical"`
}

type AcquireLeaseRequest struct {
	ClientID string   `json:"client_id" binding:"required,uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
	LeaseTTL Duration `json:"lease_ttl,omitempty" example:"30s"`
	Wait     bool     `json:"wait" example:"true"`
	Timeout  Duration `json:"timeout,omitempty" example:"5m"`
}

type RefreshLeaseRequest struct {
	ClientID string   `json:"client_id" binding:"required,uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
	LeaseID  string   `json:"lease_id" binding:"required" example:"lease-123456"`
	NewTTL   Duration `json:"new_ttl,omitempty" example:"30s"`
}

type ReleaseLeaseRequest struct {
	ClientID string `json:"client_id" binding:"required,uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
	LeaseID  string `json:"lease_id" binding:"required" example:"lease-123456"`
}

type QueueDeleteRequest struct {
	ClientID string `json:"client_id" binding:"required,uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
}

type ListLocksResponse struct {
	Locks []string `json:"locks" example:"lock1,lock2,lock3"`
}
