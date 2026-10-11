package gcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeClient is an in-memory Secret Manager with the semantics the backend
// relies on: one secret holds numbered versions, "latest" resolves to the
// highest version number whatever its state (as in Secret Manager), destroyed
// versions cannot be accessed, and the gRPC codes match the real service. Tests inject failures per method and
// read back the call log.
type fakeClient struct {
	mu      sync.Mutex
	secrets map[string]*fakeSecret // keyed by full secret name
	fail    map[string]error       // method name -> error to return once
	failAll map[string]error       // method name -> error to return every time
	calls   []string
	closed  bool

	// onList runs inside ListEnabledVersions, after the call is recorded and
	// before the listing is built; tests use it to simulate a concurrent
	// writer adding a version between a Set's AddSecretVersion and its list.
	onList func()
}

type fakeSecret struct {
	next     int64
	versions map[int64]*fakeVersion
}

type fakeVersion struct {
	data    []byte
	enabled bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{secrets: map[string]*fakeSecret{}, fail: map[string]error{}, failAll: map[string]error{}}
}

func (f *fakeClient) failNext(method string, err error)   { f.fail[method] = err }
func (f *fakeClient) failAlways(method string, err error) { f.failAll[method] = err }

func (f *fakeClient) take(method string) error {
	f.calls = append(f.calls, method)
	if err, ok := f.fail[method]; ok {
		delete(f.fail, method)
		return err
	}
	if err, ok := f.failAll[method]; ok {
		return err
	}
	return nil
}

func (f *fakeClient) count(method string) int {
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

func splitVersion(name string) (secret string, n int64, ok bool) {
	i := strings.LastIndex(name, "/versions/")
	if i < 0 {
		return "", 0, false
	}
	secret = name[:i]
	if name[i+len("/versions/"):] == "latest" {
		return secret, -1, true
	}
	_, err := fmt.Sscanf(name[i+len("/versions/"):], "%d", &n)
	return secret, n, err == nil
}

func (f *fakeClient) AccessSecretVersion(_ context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("AccessSecretVersion"); err != nil {
		return nil, err
	}
	secret, n, ok := splitVersion(req.GetName())
	s := f.secrets[secret]
	if !ok || s == nil {
		return nil, status.Error(codes.NotFound, "secret not found")
	}
	if n == -1 {
		// Secret Manager's "latest" alias is the highest version number,
		// whatever its state; a destroyed newest version makes it fail.
		n = s.next - 1
		if n < 1 {
			return nil, status.Error(codes.NotFound, "no versions")
		}
	}
	v := s.versions[n]
	if v == nil || !v.enabled {
		return nil, status.Error(codes.FailedPrecondition, "version is destroyed")
	}
	return &secretmanagerpb.AccessSecretVersionResponse{Name: fmt.Sprintf("%s/versions/%d", secret, n), Payload: &secretmanagerpb.SecretPayload{Data: v.data}}, nil
}

func (f *fakeClient) CreateSecret(_ context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("CreateSecret"); err != nil {
		return nil, err
	}
	name := req.GetParent() + "/secrets/" + req.GetSecretId()
	if _, exists := f.secrets[name]; exists {
		return nil, status.Error(codes.AlreadyExists, "secret exists")
	}
	f.secrets[name] = &fakeSecret{next: 1, versions: map[int64]*fakeVersion{}}
	return &secretmanagerpb.Secret{Name: name, Labels: req.GetSecret().GetLabels()}, nil
}

func (f *fakeClient) AddSecretVersion(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("AddSecretVersion"); err != nil {
		return nil, err
	}
	s := f.secrets[req.GetParent()]
	if s == nil {
		return nil, status.Error(codes.NotFound, "secret not found")
	}
	n := s.next
	s.next++
	s.versions[n] = &fakeVersion{data: append([]byte(nil), req.GetPayload().GetData()...), enabled: true}
	return &secretmanagerpb.SecretVersion{Name: fmt.Sprintf("%s/versions/%d", req.GetParent(), n), State: secretmanagerpb.SecretVersion_ENABLED}, nil
}

func (f *fakeClient) ListEnabledVersions(_ context.Context, secretName string) ([]*secretmanagerpb.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("ListEnabledVersions"); err != nil {
		return nil, err
	}
	if f.onList != nil {
		f.onList()
	}
	s := f.secrets[secretName]
	if s == nil {
		return nil, status.Error(codes.NotFound, "secret not found")
	}
	var out []*secretmanagerpb.SecretVersion
	for i := int64(1); i < s.next; i++ {
		if v := s.versions[i]; v != nil && v.enabled {
			out = append(out, &secretmanagerpb.SecretVersion{Name: fmt.Sprintf("%s/versions/%d", secretName, i), State: secretmanagerpb.SecretVersion_ENABLED})
		}
	}
	return out, nil
}

func (f *fakeClient) DestroySecretVersion(_ context.Context, req *secretmanagerpb.DestroySecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("DestroySecretVersion"); err != nil {
		return nil, err
	}
	secret, n, ok := splitVersion(req.GetName())
	s := f.secrets[secret]
	if !ok || s == nil || s.versions[n] == nil {
		return nil, status.Error(codes.NotFound, "version not found")
	}
	if !s.versions[n].enabled {
		return nil, status.Error(codes.FailedPrecondition, "already destroyed")
	}
	s.versions[n].enabled = false
	return &secretmanagerpb.SecretVersion{Name: req.GetName(), State: secretmanagerpb.SecretVersion_DESTROYED}, nil
}

func (f *fakeClient) DeleteSecret(_ context.Context, req *secretmanagerpb.DeleteSecretRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("DeleteSecret"); err != nil {
		return err
	}
	if _, ok := f.secrets[req.GetName()]; !ok {
		return status.Error(codes.NotFound, "secret not found")
	}
	delete(f.secrets, req.GetName())
	return nil
}

func (f *fakeClient) Close() error { f.closed = true; return nil }

// enabledVersions reports the enabled version numbers of a secret, for assertions.
func (f *fakeClient) enabledVersions(secretName string) []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.secrets[secretName]
	if s == nil {
		return nil
	}
	var out []int64
	for i := int64(1); i < s.next; i++ {
		if v := s.versions[i]; v != nil && v.enabled {
			out = append(out, i)
		}
	}
	return out
}

// addVersionLocked appends an enabled version without taking the lock; for
// use from onList, which runs with the lock held.
func (f *fakeClient) addVersionLocked(secretName, data string) {
	s := f.secrets[secretName]
	n := s.next
	s.next++
	s.versions[n] = &fakeVersion{data: []byte(data), enabled: true}
}
