package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter(g *GlockServer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/create", func(c *gin.Context) { CreateHandler(c, g) })
	r.POST("/update", func(c *gin.Context) { UpdateHandler(c, g) })
	r.DELETE("/delete/:name", func(c *gin.Context) { DeleteHandler(c, g) })
	r.POST("/acquire", func(c *gin.Context) { AcquireHandler(c, g) })
	r.POST("/refresh", func(c *gin.Context) { RefreshHandler(c, g) })
	r.POST("/verify", func(c *gin.Context) { VerifyHandler(c, g) })
	r.POST("/release", func(c *gin.Context) { ReleaseHandler(c, g) })
	r.GET("/status", func(c *gin.Context) { StatusHandler(c, g) })
	r.GET("/list", func(c *gin.Context) { ListHandler(c, g) })
	return r
}

func TestHandlers_CreateAcquireRelease(t *testing.T) {
	config := DefaultConfig()
	g := &GlockServer{
		Capacity: 10,
		Locks:    sync.Map{},
		Config:   config,
	}
	r := setupRouter(g)

	// create
	cr := CreateRequest{Name: "a", TTL: "1s", MaxTTL: "1m"}
	b, _ := json.Marshal(cr)
	req := httptest.NewRequest("POST", "/create", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create handler failed: %d %s", w.Code, w.Body.String())
	}

	// acquire
	ar := AcquireRequest{Name: "a", Owner: "me", OwnerID: "11111111-1111-1111-1111-111111111111"}
	b, _ = json.Marshal(ar)
	req = httptest.NewRequest("POST", "/acquire", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("acquire handler failed: %d %s", w.Code, w.Body.String())
	}
	// Parse response and get lock.Token
	var acquireResp AcquireResponse
	if err := json.Unmarshal(w.Body.Bytes(), &acquireResp); err != nil {
		t.Fatalf("failed to parse acquire response: %v", err)
	}
	token := acquireResp.Lock.Token

	// release
	rr := ReleaseRequest{Name: "a", OwnerID: "11111111-1111-1111-1111-111111111111", Token: token}
	b, _ = json.Marshal(rr)
	req = httptest.NewRequest("POST", "/release", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("release handler failed: %d %s", w.Code, w.Body.String())
	}
}

func TestHandlers_Status(t *testing.T) {
	config := DefaultConfig()
	g := &GlockServer{
		Capacity: 10,
		Locks:    sync.Map{},
		Config:   config,
	}
	r := setupRouter(g)

	// Create multiple locks
	locks := []string{"a", "b", "c"}
	for _, name := range locks {
		cr := CreateRequest{Name: name, TTL: "1s", MaxTTL: "1m"}
		b, _ := json.Marshal(cr)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/create", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("create handler failed: %d %s", w.Code, w.Body.String())
		}
	}

	// Check list
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/list", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list handler failed: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		Locks []*string `json:"locks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(resp.Locks) != len(locks) {
		t.Fatalf("expected %d locks, got %d", len(locks), len(resp.Locks))
	}
}
