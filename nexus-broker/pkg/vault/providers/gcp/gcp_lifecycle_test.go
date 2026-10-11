package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/providers"
)

const (
	testProject = "proj"
	testPrefix  = "nexus-providers"
	testKey     = "11111111-2222-3333-4444-555555555555"
)

func newTestBackend() (*Backend, *fakeClient) {
	f := newFakeClient()
	return newWithClient(f, testProject, testPrefix), f
}

func TestGet_NotFound(t *testing.T) {
	b, _ := newTestBackend()
	if _, err := b.Get(context.Background(), testKey); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSet_CreatesSecretThenAddsVersion(t *testing.T) {
	b, f := newTestBackend()
	if err := b.Set(context.Background(), testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateSecret") != 1 || f.count("AddSecretVersion") != 1 {
		t.Fatalf("calls: %v", f.calls)
	}
	got, err := b.Get(context.Background(), testKey)
	if err != nil || got != "v1" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	name := b.secretName(testKey)
	if !strings.HasSuffix(name, "/secrets/"+testPrefix+"-"+testKey) {
		t.Fatalf("secret name %q", name)
	}
}

func TestSet_AlreadyExistsIsNotAnError(t *testing.T) {
	b, f := newTestBackend()
	if err := b.Set(context.Background(), testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	// Second Set: CreateSecret answers AlreadyExists (a concurrent writer or
	// a previous run); the version must still be added.
	if err := b.Set(context.Background(), testKey, "v2"); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateSecret") != 2 {
		t.Fatalf("CreateSecret calls = %d", f.count("CreateSecret"))
	}
	if got, _ := b.Get(context.Background(), testKey); got != "v2" {
		t.Fatalf("Get = %q, want v2", got)
	}
}

func TestSet_RetiresOnlyOlderVersions(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	for _, v := range []string{"v1", "v2", "v3"} {
		if err := b.Set(ctx, testKey, v); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.enabledVersions(b.secretName(testKey)); len(got) != 1 || got[0] != 3 {
		t.Fatalf("enabled versions after three writes = %v, want [3]", got)
	}

	// A concurrent writer adds version 5 between our AddSecretVersion (4)
	// and our listing: it must survive our retirement pass.
	name := b.secretName(testKey)
	f.mu.Lock()
	f.secrets[name].versions[4] = &fakeVersion{data: []byte("ours"), enabled: true}
	f.secrets[name].versions[5] = &fakeVersion{data: []byte("theirs"), enabled: true}
	f.secrets[name].next = 6
	f.mu.Unlock()
	// Simulate our Set having produced version 4 by retiring relative to it.
	versions, _ := f.ListEnabledVersions(ctx, name)
	for _, v := range versions {
		if versionOlder(v.GetName(), name+"/versions/4") {
			_, _ = f.DestroySecretVersion(ctx, &secretmanagerpb.DestroySecretVersionRequest{Name: v.GetName()})
		}
	}
	if got := f.enabledVersions(name); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("enabled = %v, want [4 5]: the newer concurrent version must not be destroyed", got)
	}
}

func TestSet_DestroyRaceIsIdempotent(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if err := b.Set(ctx, testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	// Another rotation destroyed version 1 just before ours tries to.
	f.failNext("DestroySecretVersion", status.Error(codes.FailedPrecondition, "already destroyed"))
	if err := b.Set(ctx, testKey, "v2"); err != nil {
		t.Fatalf("FailedPrecondition on destroy must count as success, got %v", err)
	}
	f.failNext("DestroySecretVersion", status.Error(codes.NotFound, "gone"))
	if err := b.Set(ctx, testKey, "v3"); err != nil {
		t.Fatalf("NotFound on destroy must count as success, got %v", err)
	}
}

func TestSet_ListFailureIsReportedButValueIsLive(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if err := b.Set(ctx, testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	f.failNext("ListEnabledVersions", status.Error(codes.Unavailable, "backend hiccup"))
	err := b.Set(ctx, testKey, "v2")
	if err == nil || !strings.Contains(err.Error(), "earlier versions were not retired") {
		t.Fatalf("want a retirement error, got %v", err)
	}
	if got, _ := b.Get(ctx, testKey); got != "v2" {
		t.Fatalf("the new version must be live even when retirement failed, got %q", got)
	}
	if got := f.enabledVersions(b.secretName(testKey)); len(got) != 2 {
		t.Fatalf("v1 should still be enabled after a failed listing, enabled = %v", got)
	}
}

func TestSet_AddVersionFailureIsAnError(t *testing.T) {
	b, f := newTestBackend()
	f.failNext("AddSecretVersion", status.Error(codes.PermissionDenied, "no versions.add"))
	err := b.Set(context.Background(), testKey, "v1")
	if err == nil || status.Code(errors.Unwrap(errors.Unwrap(err))) != codes.PermissionDenied && !strings.Contains(err.Error(), "no versions.add") {
		t.Fatalf("want the permission error surfaced, got %v", err)
	}
}

func TestDelete_MissingIsFine(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if err := b.Delete(ctx, testKey); err != nil {
		t.Fatalf("deleting a missing secret must succeed, got %v", err)
	}
	if err := b.Set(ctx, testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, testKey); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, testKey); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("after Delete, Get must be ErrNotFound, got %v", err)
	}
	if f.count("DeleteSecret") != 2 {
		t.Fatalf("DeleteSecret calls = %d", f.count("DeleteSecret"))
	}
}

func TestPing_LeavesNothingBehind(t *testing.T) {
	b, f := newTestBackend()
	if err := b.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(f.secrets); n != 0 {
		t.Fatalf("probe secret left behind: %d secret(s)", n)
	}
	// Two writes, so a rotation (DestroySecretVersion) was exercised.
	if f.count("AddSecretVersion") != 2 || f.count("DestroySecretVersion") != 1 || f.count("DeleteSecret") < 1 {
		t.Fatalf("probe calls: %v", f.calls)
	}
}

func TestPing_FailsFastAndCleansUpOnMissingPermission(t *testing.T) {
	b, f := newTestBackend()
	f.failNext("AddSecretVersion", status.Error(codes.PermissionDenied, "no versions.add"))
	err := b.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no versions.add") {
		t.Fatalf("want the permission error, got %v", err)
	}
	if len(f.secrets) != 0 {
		t.Fatal("the probe container must be deleted even when the probe fails")
	}
}

func TestPing_ReportsCleanupFailure(t *testing.T) {
	b, f := newTestBackend()
	// The probe itself succeeds up to its own Delete, which fails; the
	// deferred cleanup then also fails. Both must be reported, naming the id.
	f.failNext("DeleteSecret", status.Error(codes.PermissionDenied, "no secrets.delete"))
	err := b.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no secrets.delete") {
		t.Fatalf("want the delete error, got %v", err)
	}
}

func TestClose_ClosesClient(t *testing.T) {
	b, f := newTestBackend()
	if err := b.Close(); err != nil || !f.closed {
		t.Fatalf("Close: err=%v closed=%v", err, f.closed)
	}
}
