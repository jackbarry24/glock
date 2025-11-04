package core

import (
	"context"
	"errors"
	"time"
)

var ErrNotImplemented = errors.New("not implemented")

// Server is the shard manager and public API used by HTTP layer.
type Server struct {
	cfg       ServerConfig
	shards    []*Shard
	startTime time.Time
	clock     Clock
}

// NewServer constructs shard goroutines but does not start them.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.NumShards <= 0 {
		cfg.NumShards = 32
	}
	if cfg.ExpiryTick <= 0 {
		cfg.ExpiryTick = 50 * time.Millisecond
	}
	s := &Server{
		cfg:       cfg,
		startTime: time.Now(),
		clock:     RealClock{},
	}
	s.initShards()
	return s, nil
}

// Start runs shard workers.
func (s *Server) Start() {
	for _, sh := range s.shards {
		go sh.run()
	}
}

// Stop signals shards to stop and waits for them to finish with context timeout.
func (s *Server) Stop(ctx context.Context) {
	for _, sh := range s.shards {
		sh.stop()
	}

	doneCh := make(chan struct{})
	go func() {
		for _, sh := range s.shards {
			<-sh.stopCh
		}
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-ctx.Done():
	}
}

// ---- Public API called by HTTP handlers (signatures) ----

func (s *Server) Create(lockID string, cfg LockConfig, tags []string, idemKey string) (CreateResp, error) {
	return s.routeCreate(lockID, cfg, tags, idemKey)
}

func (s *Server) Acquire(in AcquireReq) (AcquireResp, error) {
	return s.routeAcquire(in)
}

func (s *Server) Refresh(in RefreshReq) (RefreshResp, error) {
	return s.routeRefresh(in)
}

func (s *Server) Release(in ReleaseReq) (ReleaseResp, error) {
	return s.routeRelease(in)
}

func (s *Server) Status(lockID string) (StatusResp, error) {
	return s.routeStatus(lockID)
}

func (s *Server) QueueGet(lockID string, limit int) (QueueGetResp, error) {
	return s.routeQueueGet(lockID, limit)
}

func (s *Server) QueueDel(lockID, waitID, clientID string) (QueueDelResp, error) {
	return s.routeQueueDel(lockID, waitID, clientID)
}

func (s *Server) Delete(lockID string) (DeleteResp, error) {
	return s.routeDelete(lockID)
}

func (s *Server) List() ([]string, error) {
	allLocks := make([]string, 0)

	for _, sh := range s.shards {
		replyCh := make(chan []string, 1)
		sh.msgCh <- listReq{Reply: replyCh}
		locks := <-replyCh
		allLocks = append(allLocks, locks...)
	}

	return allLocks, nil
}

func (s *Server) Metrics(lockID string) (MetricsResp, error) {
	return s.routeMetrics(lockID)
}

func (s *Server) AllStatus() ([]LockStatus, error) {
	allStatuses := make([]LockStatus, 0)

	for _, sh := range s.shards {
		replyCh := make(chan AllStatusResp, 1)
		sh.msgCh <- AllStatusReq{Reply: replyCh}
		resp := <-replyCh
		if resp.Err != nil {
			return nil, resp.Err
		}
		allStatuses = append(allStatuses, resp.Statuses...)
	}

	return allStatuses, nil
}

// ---- internal routing helpers (signatures) ----

func (s *Server) shardOf(lockID string) *Shard {
	idx := fnv32(lockID) % uint32(len(s.shards))
	return s.shards[idx]
}

func (s *Server) initShards() {
	s.shards = make([]*Shard, s.cfg.NumShards)
	for i := range s.shards {
		s.shards[i] = newShard(i, s.cfg, s.clock)
	}
}

func (s *Server) routeCreate(lockID string, cfg LockConfig, tags []string, idemKey string) (CreateResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan CreateResp, 1)
	req := CreateReq{
		LockID:  lockID,
		Config:  cfg,
		Tags:    tags,
		IdemKey: idemKey,
		Reply:   replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}

func (s *Server) routeAcquire(in AcquireReq) (AcquireResp, error) {
	sh := s.shardOf(in.LockID)
	in.Reply = make(chan AcquireResp, 1)
	sh.msgCh <- in
	resp := <-in.Reply
	return resp, resp.Err
}

func (s *Server) routeRefresh(in RefreshReq) (RefreshResp, error) {
	sh := s.shardOf(in.LockID)
	in.Reply = make(chan RefreshResp, 1)
	sh.msgCh <- in
	resp := <-in.Reply
	return resp, resp.Err
}

func (s *Server) routeRelease(in ReleaseReq) (ReleaseResp, error) {
	sh := s.shardOf(in.LockID)
	in.Reply = make(chan ReleaseResp, 1)
	sh.msgCh <- in
	resp := <-in.Reply
	return resp, resp.Err
}

func (s *Server) routeStatus(lockID string) (StatusResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan StatusResp, 1)
	req := StatusReq{
		LockID: lockID,
		Reply:  replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}

func (s *Server) routeQueueGet(lockID string, limit int) (QueueGetResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan QueueGetResp, 1)
	req := QueueGetReq{
		LockID: lockID,
		Limit:  limit,
		Reply:  replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}

func (s *Server) routeQueueDel(lockID, waitID, clientID string) (QueueDelResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan QueueDelResp, 1)
	req := QueueDelReq{
		LockID:   lockID,
		WaitID:   waitID,
		ClientID: clientID,
		Reply:    replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}

func (s *Server) routeDelete(lockID string) (DeleteResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan DeleteResp, 1)
	req := DeleteReq{
		LockID: lockID,
		Reply:  replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}

func (s *Server) routeMetrics(lockID string) (MetricsResp, error) {
	sh := s.shardOf(lockID)
	replyCh := make(chan MetricsResp, 1)
	req := MetricsReq{
		LockID: lockID,
		Reply:  replyCh,
	}
	sh.msgCh <- req
	resp := <-replyCh
	return resp, resp.Err
}
