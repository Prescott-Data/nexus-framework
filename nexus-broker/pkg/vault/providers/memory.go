package providers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Memory is an in-process Backend. It exists for tests and for exercising the
// external-backend code paths without a cloud dependency. It keeps numbered
// versions per key, like Secret Manager, so rotation and retirement can be
// asserted. It reports KindInternal because, like the column, it does not
// survive the process.
type Memory struct {
	mu       sync.RWMutex
	versions map[string][]memVersion // key -> versions, index+1 is the number

	// PingErr, when set, is returned by Ping. Tests use it to simulate a
	// backend that is configured but unreachable.
	PingErr error
}

type memVersion struct {
	value   string
	retired bool
}

// NewMemory returns an empty in-memory backend.
func NewMemory() *Memory {
	return &Memory{versions: make(map[string][]memVersion)}
}

func (m *Memory) Kind() Kind { return KindInternal }

// MemoryRef is the reference format: "mem:<key>:<version number>".
func MemoryRef(key string, n int) string { return fmt.Sprintf("mem:%s:%d", key, n) }

func parseMemoryRef(ref string) (key string, n int, ok bool) {
	var k string
	var num int
	if _, err := fmt.Sscanf(ref, "mem:%36s:%d", &k, &num); err != nil {
		// keys are not always UUIDs in tests; fall back to a split
		parts := strings.Split(ref, ":")
		if len(parts) != 3 || parts[0] != "mem" {
			return "", 0, false
		}
		num, err = strconv.Atoi(parts[2])
		if err != nil {
			return "", 0, false
		}
		return parts[1], num, true
	}
	return k, num, true
}

func (m *Memory) Current(_ context.Context, key string) (string, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vs := m.versions[key]
	for i := len(vs) - 1; i >= 0; i-- {
		if !vs[i].retired {
			return vs[i].value, MemoryRef(key, i+1), nil
		}
	}
	return "", "", ErrNotFound
}

func (m *Memory) GetRef(_ context.Context, ref string) (string, error) {
	key, n, ok := parseMemoryRef(ref)
	if !ok {
		return "", ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	vs := m.versions[key]
	if n < 1 || n > len(vs) || vs[n-1].retired {
		return "", ErrNotFound
	}
	return vs[n-1].value, nil
}

func (m *Memory) Set(_ context.Context, key, value string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.versions[key] = append(m.versions[key], memVersion{value: value})
	return MemoryRef(key, len(m.versions[key])), nil
}

func (m *Memory) Retire(_ context.Context, key, keepRef string) error {
	_, keep, ok := parseMemoryRef(keepRef)
	if !ok {
		return fmt.Errorf("memory backend: bad ref %q", keepRef)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := m.versions[key]
	for i := range vs {
		if i+1 < keep {
			vs[i].retired = true
		}
	}
	return nil
}

func (m *Memory) DiscardRef(_ context.Context, ref string) error {
	key, n, ok := parseMemoryRef(ref)
	if !ok {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if vs := m.versions[key]; n >= 1 && n <= len(vs) {
		vs[n-1].retired = true
	}
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.versions, key)
	return nil
}

func (m *Memory) Ping(context.Context) error { return m.PingErr }

// Len reports how many keys hold a secret. Tests use it to assert cleanup.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.versions)
}

// LiveVersions reports how many versions of key are not retired.
func (m *Memory) LiveVersions(key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, v := range m.versions[key] {
		if !v.retired {
			n++
		}
	}
	return n
}
