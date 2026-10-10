package providers

import (
	"context"
	"sync"
)

// Memory is an in-process Backend. It exists for tests and for exercising the
// external-backend code paths without a cloud dependency. It reports
// KindInternal because, like the column, it does not survive the process.
type Memory struct {
	mu      sync.RWMutex
	secrets map[string]string

	// PingErr, when set, is returned by Ping. Tests use it to simulate a
	// backend that is configured but unreachable.
	PingErr error
}

// NewMemory returns an empty in-memory backend.
func NewMemory() *Memory {
	return &Memory{secrets: make(map[string]string)}
}

func (m *Memory) Kind() Kind { return KindInternal }

func (m *Memory) Get(_ context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.secrets[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *Memory) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[key] = value
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.secrets, key)
	return nil
}

func (m *Memory) Ping(context.Context) error { return m.PingErr }

// Len reports how many secrets are held. Tests use it to assert cleanup.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.secrets)
}
