package glock

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	servercore "glock-server/core"

	"github.com/gin-gonic/gin"
)

type testServer struct {
	httpServer *http.Server
	url        string
	stop       func()
}

func startTestServer(t *testing.T) *testServer {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv, err := servercore.NewServer(servercore.ServerConfig{NumShards: 4})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()

	r := gin.New()
	api := r.Group("/api")
	locks := api.Group("/locks")
	{
		locks.GET("", servercore.ListHandler(srv))
		locks.POST("", servercore.CreateHandler(srv))
		locks.GET("/:lock_id", servercore.StatusHandler(srv))
		locks.DELETE("/:lock_id", servercore.DeleteHandler(srv))
		locks.GET("/:lock_id/metrics", servercore.MetricsHandler(srv))
		locks.POST("/:lock_id/lease", servercore.AcquireHandler(srv))
		locks.PUT("/:lock_id/lease", servercore.RefreshHandler(srv))
		locks.DELETE("/:lock_id/lease", servercore.ReleaseHandler(srv))
		locks.GET("/:lock_id/queue", servercore.QueueGetHandler(srv))
		locks.DELETE("/:lock_id/queue/:wait_id", servercore.QueueDelHandler(srv))
	}

	httpSrv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: r}
	go func() { _ = httpSrv.ListenAndServe() }()

	return &testServer{
		httpServer: httpSrv,
		url:        fmt.Sprintf("http://127.0.0.1:%d", port),
		stop: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(ctx)
		},
	}
}

func (s *testServer) Close() {
	if s != nil && s.stop != nil {
		s.stop()
	}
}
func (s *testServer) URL() string { return s.url }

func TestBasicLifecycle(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Close()

	c, err := Connect(ts.URL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	created, err := c.CreateLock("lock-a", LockConfig{DefaultTTL: "200ms", MaxTTL: "2s"}, nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}

	res, err := c.Acquire("lock-a", AcquireOptions{Wait: false, LeaseTTL: "200ms"})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if res.State != "granted" || res.Lease == nil {
		t.Fatalf("expected granted, got %+v", res)
	}

	_, err = c.Refresh("lock-a", res.Lease.LeaseID, "200ms", "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	_, err = c.Release("lock-a", res.Lease.LeaseID, "")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
}
