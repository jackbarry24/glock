package glock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

type Glock struct {
	ServerURL  string
	ID         string
	httpClient *http.Client
}

type ClientConfig struct {
	ServerURL  string
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
	HTTPClient *http.Client
}

func DefaultClientConfig(serverURL string) *ClientConfig {
	return &ClientConfig{
		ServerURL:  serverURL,
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *Glock) getHTTPClient() *http.Client {
	if g.httpClient != nil {
		return g.httpClient
	}
	return http.DefaultClient
}

func Connect(serverURL string) (*Glock, error) {
	config := DefaultClientConfig(serverURL)
	return ConnectWithConfig(config)
}

func ConnectWithConfig(config *ClientConfig) (*Glock, error) {
	if config.ServerURL == "" {
		return nil, fmt.Errorf("server URL must not be empty")
	}
	return &Glock{
		ServerURL:  config.ServerURL,
		ID:         uuid.NewString(),
		httpClient: config.HTTPClient,
	}, nil
}

func (g *Glock) apiBase() string { return g.ServerURL + "/api" }

func (g *Glock) doJSON(method, path string, body any, headers map[string]string, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewBuffer(b)
	}
	req, err := http.NewRequest(method, g.apiBase()+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := g.getHTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (g *Glock) CreateLock(lockID string, cfg LockConfig, tags []string, idemKey string) (bool, error) {
	req := CreateLockRequest{LockID: lockID, Config: cfg, Tags: tags}
	headers := map[string]string{}
	if idemKey != "" {
		headers["Idempotency-Key"] = idemKey
	}
	var resp struct {
		LockID  string      `json:"lock_id"`
		Config  interface{} `json:"config"`
		Created bool        `json:"created"`
	}
	code, err := g.doJSON(http.MethodPost, "/locks", req, headers, &resp)
	if err != nil {
		return false, err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return false, fmt.Errorf("create failed: %d", code)
	}
	return resp.Created, nil
}

type AcquireOptions struct {
	LeaseTTL string
	Wait     bool
	Timeout  string
	IdemKey  string
}
type AcquireResult struct {
	State         string
	Lease         *Lease
	Queue         *QueueInfo
	OwnerClientID string
	ExpiresAt     time.Time
}

func (g *Glock) Acquire(lockID string, opts AcquireOptions) (*AcquireResult, error) {
	req := AcquireLeaseRequest{ClientID: g.ID, LeaseTTL: opts.LeaseTTL, Wait: opts.Wait, Timeout: opts.Timeout}
	headers := map[string]string{}
	if opts.IdemKey != "" {
		headers["Idempotency-Key"] = opts.IdemKey
	}
	var resp AcquireResp
	code, err := g.doJSON(http.MethodPost, "/locks/"+url.PathEscape(lockID)+"/lease", req, headers, &resp)
	if err != nil {
		return nil, err
	}
	res := &AcquireResult{State: resp.State}
	switch resp.State {
	case "granted":
		res.Lease = &Lease{LockID: lockID, LeaseID: resp.LeaseID, ExpiresAt: resp.ExpiresAt, FenceToken: resp.FenceToken}
		return res, nil
	case "queued":
		res.Queue = &QueueInfo{WaitID: resp.WaitID, Position: resp.Position, EstimatedWaitMs: resp.EstimatedWaitMs}
		return res, nil
	case "busy":
		res.OwnerClientID, res.ExpiresAt = resp.OwnerClientID, resp.ExpiresAt
		if code != http.StatusConflict {
			return res, fmt.Errorf("unexpected status for busy: %d", code)
		}
		return res, nil
	case "not_found":
		return nil, fmt.Errorf("lock not found")
	default:
		return nil, fmt.Errorf("unknown acquire state: %s", resp.State)
	}
}

func (g *Glock) Refresh(lockID, leaseID, newTTL, idemKey string) (*RefreshResp, error) {
	req := RefreshLeaseRequest{ClientID: g.ID, LeaseID: leaseID, NewTTL: newTTL}
	headers := map[string]string{}
	if idemKey != "" {
		headers["Idempotency-Key"] = idemKey
	}
	var resp RefreshResp
	code, err := g.doJSON(http.MethodPut, "/locks/"+url.PathEscape(lockID)+"/lease", req, headers, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("refresh failed: %d", code)
	}
	return &resp, nil
}

func (g *Glock) Release(lockID, leaseID, idemKey string) (*ReleaseResp, error) {
	req := ReleaseLeaseRequest{ClientID: g.ID, LeaseID: leaseID}
	headers := map[string]string{}
	if idemKey != "" {
		headers["Idempotency-Key"] = idemKey
	}
	var resp ReleaseResp
	code, err := g.doJSON(http.MethodDelete, "/locks/"+url.PathEscape(lockID)+"/lease", req, headers, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("release failed: %d", code)
	}
	return &resp, nil
}

func (g *Glock) Status(lockID string) (*StatusResp, error) {
	var resp StatusResp
	code, err := g.doJSON(http.MethodGet, "/locks/"+url.PathEscape(lockID), nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("status failed: %d", code)
	}
	return &resp, nil
}

func (g *Glock) ListLocks() ([]string, error) {
	var resp struct {
		Locks []string `json:"locks"`
	}
	code, err := g.doJSON(http.MethodGet, "/locks", nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("list failed: %d", code)
	}
	return resp.Locks, nil
}

func (g *Glock) DeleteLock(lockID string) error {
	var resp struct {
		Deleted bool `json:"deleted"`
	}
	code, err := g.doJSON(http.MethodDelete, "/locks/"+url.PathEscape(lockID), nil, nil, &resp)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("delete failed: %d", code)
	}
	if !resp.Deleted {
		return fmt.Errorf("lock not deleted")
	}
	return nil
}

func (g *Glock) QueueGet(lockID string, limit int) ([]WaitEntryView, error) {
	path := "/locks/" + url.PathEscape(lockID) + "/queue"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var resp QueueGetResp
	code, err := g.doJSON(http.MethodGet, path, nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("queue get failed: %d", code)
	}
	return resp.Queue, nil
}

func (g *Glock) QueueCancel(lockID, waitID string) error {
	body := struct {
		ClientID string `json:"client_id"`
	}{ClientID: g.ID}
	code, err := g.doJSON(http.MethodDelete, "/locks/"+url.PathEscape(lockID)+"/queue/"+url.PathEscape(waitID), body, nil, &struct{}{})
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("queue cancel failed: %d", code)
	}
	return nil
}

func (g *Glock) AcquireOrWait(lockID string, leaseTTL string, timeout time.Duration, pollInterval time.Duration) (*Lease, error) {
	res, err := g.Acquire(lockID, AcquireOptions{LeaseTTL: leaseTTL, Wait: true})
	if err != nil {
		return nil, err
	}
	if res.State == "granted" && res.Lease != nil {
		return res.Lease, nil
	}
	if res.State != "queued" || res.Queue == nil {
		return nil, fmt.Errorf("unexpected state: %s", res.State)
	}
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			_ = g.QueueCancel(lockID, res.Queue.WaitID)
			return nil, fmt.Errorf("timeout waiting for lock")
		}
		st, err := g.Status(lockID)
		if err != nil {
			return nil, err
		}
		if st.Status.Owner != nil && st.Status.Owner.ClientID == g.ID {
			return &Lease{LockID: lockID, LeaseID: st.Status.Owner.LeaseID, ExpiresAt: st.Status.Owner.ExpiresAt, FenceToken: st.Status.Owner.FenceToken}, nil
		}
		time.Sleep(pollInterval)
	}
}
