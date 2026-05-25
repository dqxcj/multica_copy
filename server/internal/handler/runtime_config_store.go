package handler

import (
	"context"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
)

type ConfigRequestStatus string

const (
	ConfigRequestPending   ConfigRequestStatus = "pending"
	ConfigRequestRunning   ConfigRequestStatus = "running"
	ConfigRequestCompleted ConfigRequestStatus = "completed"
	ConfigRequestFailed    ConfigRequestStatus = "failed"
	ConfigRequestTimeout   ConfigRequestStatus = "timeout"
)

const runtimeConfigStoreRetention = 5 * time.Minute

type RuntimeConfigReadRequest struct {
	ID        string                  `json:"id"`
	RuntimeID string                  `json:"runtime_id"`
	Provider  string                  `json:"provider"`
	Status    ConfigRequestStatus     `json:"status"`
	Configs   *daemon.ProviderConfigs `json:"configs,omitempty"`
	Error     string                  `json:"error,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

type RuntimeConfigWriteRequest struct {
	ID        string                  `json:"id"`
	RuntimeID string                  `json:"runtime_id"`
	Provider  string                  `json:"provider"`
	Configs   *daemon.ProviderConfigs `json:"configs"`
	Status    ConfigRequestStatus     `json:"status"`
	Backups   []string                `json:"backups,omitempty"`
	Error     string                  `json:"error,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// ConfigReadStore tracks pending / running / completed config read requests
// between the server and daemon. The server MUST stay stateless — any state
// that needs to outlive a single request has to live in shared storage so
// multi-node deploys can have POST, heartbeat and poll land on different
// nodes and still agree on the request's state.
type ConfigReadStore interface {
	Create(ctx context.Context, runtimeID, provider string) (*RuntimeConfigReadRequest, error)
	Get(ctx context.Context, id string) (*RuntimeConfigReadRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeConfigReadRequest, error)
	Complete(ctx context.Context, id string, configs *daemon.ProviderConfigs) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// ConfigWriteStore is the same contract as ConfigReadStore but for config
// write requests. The Create signature carries write-specific fields (configs).
type ConfigWriteStore interface {
	Create(ctx context.Context, runtimeID, provider string, configs *daemon.ProviderConfigs) (*RuntimeConfigWriteRequest, error)
	Get(ctx context.Context, id string) (*RuntimeConfigWriteRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeConfigWriteRequest, error)
	Complete(ctx context.Context, id string, backups []string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// InMemoryConfigReadStore is the single-node implementation — good enough for
// local dev and the in-process test suite. Production (multi-node) must use
// a Redis-backed store so every API node agrees on the same pending set.
type InMemoryConfigReadStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeConfigReadRequest
}

func NewInMemoryConfigReadStore() *InMemoryConfigReadStore {
	return &InMemoryConfigReadStore{requests: make(map[string]*RuntimeConfigReadRequest)}
}

func (s *InMemoryConfigReadStore) Create(_ context.Context, runtimeID, provider string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Retention sweep: remove requests older than the retention window so
	// stale entries do not accumulate indefinitely.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > runtimeConfigStoreRetention {
			delete(s.requests, id)
		}
	}

	req := &RuntimeConfigReadRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Provider:  provider,
		Status:    ConfigRequestPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryConfigReadStore) Get(_ context.Context, id string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	return req, nil
}

func (s *InMemoryConfigReadStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == ConfigRequestPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryConfigReadStore) PopPending(_ context.Context, runtimeID string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeConfigReadRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == ConfigRequestPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ConfigRequestRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest, nil
}

func (s *InMemoryConfigReadStore) Complete(_ context.Context, id string, configs *daemon.ProviderConfigs) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ConfigRequestCompleted
		req.Configs = configs
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryConfigReadStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ConfigRequestFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// InMemoryConfigWriteStore mirrors InMemoryConfigReadStore for write requests.
type InMemoryConfigWriteStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeConfigWriteRequest
}

func NewInMemoryConfigWriteStore() *InMemoryConfigWriteStore {
	return &InMemoryConfigWriteStore{requests: make(map[string]*RuntimeConfigWriteRequest)}
}

func (s *InMemoryConfigWriteStore) Create(_ context.Context, runtimeID, provider string, configs *daemon.ProviderConfigs) (*RuntimeConfigWriteRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Retention sweep: remove stale entries for the same runtime so old
	// timed-out running requests do not block new ones.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > runtimeConfigStoreRetention {
			delete(s.requests, id)
		}
	}

	req := &RuntimeConfigWriteRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Provider:  provider,
		Configs:   configs,
		Status:    ConfigRequestPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryConfigWriteStore) Get(_ context.Context, id string) (*RuntimeConfigWriteRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	return req, nil
}

func (s *InMemoryConfigWriteStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == ConfigRequestPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryConfigWriteStore) PopPending(_ context.Context, runtimeID string) (*RuntimeConfigWriteRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeConfigWriteRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == ConfigRequestPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ConfigRequestRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest, nil
}

func (s *InMemoryConfigWriteStore) Complete(_ context.Context, id string, backups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ConfigRequestCompleted
		req.Backups = backups
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryConfigWriteStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ConfigRequestFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}
