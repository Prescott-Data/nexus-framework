package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	ref, err := b.Set(context.Background(), testKey, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ref, "/versions/1") {
		t.Fatalf("ref = %q, want a version resource name", ref)
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
	if _, err := b.Set(context.Background(), testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	// Second Set: CreateSecret answers AlreadyExists (a concurrent writer or
	// a previous run); the version must still be added.
	if _, err := b.Set(context.Background(), testKey, "v2"); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateSecret") != 2 {
		t.Fatalf("CreateSecret calls = %d", f.count("CreateSecret"))
	}
	if got, _ := b.Get(context.Background(), testKey); got != "v2" {
		t.Fatalf("Get = %q, want v2", got)
	}
}

func TestRetire_KeepsReferencedAndNewerVersions(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	var refs []string
	for _, v := range []string{"v1", "v2", "v3"} {
		ref, err := b.Set(ctx, testKey, v)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	name := b.secretName(testKey)
	if got := f.enabledVersions(name); len(got) != 3 {
		t.Fatalf("Set must not retire on its own; enabled = %v", got)
	}
	// Every version is still readable by reference until Retire.
	if v, err := b.GetRef(ctx, refs[0]); err != nil || v != "v1" {
		t.Fatalf("GetRef(v1) before retire = %q, %v", v, err)
	}

	// A concurrent writer adds version 4 between our Set (3) and our Retire,
	// injected from inside the listing so the real Retire path is what keeps
	// it alive.
	f.onList = func() { f.addVersionLocked(name, "theirs") }
	if err := b.Retire(ctx, testKey, refs[2]); err != nil {
		t.Fatal(err)
	}
	f.onList = nil
	if got := f.enabledVersions(name); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("enabled = %v, want [3 4]: the kept and the newer concurrent version survive", got)
	}
	if _, err := b.GetRef(ctx, refs[0]); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("a retired version must read as ErrNotFound, got %v", err)
	}
}

func TestRetire_DestroyRaceIsIdempotent(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if _, err := b.Set(ctx, testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	ref, err := b.Set(ctx, testKey, "v2")
	if err != nil {
		t.Fatal(err)
	}
	// Another rotation destroyed version 1 just before ours tries to.
	f.failNext("DestroySecretVersion", status.Error(codes.FailedPrecondition, "already destroyed"))
	if err := b.Retire(ctx, testKey, ref); err != nil {
		t.Fatalf("FailedPrecondition on destroy must count as success, got %v", err)
	}
	f.failNext("DestroySecretVersion", status.Error(codes.NotFound, "gone"))
	if err := b.Retire(ctx, testKey, ref); err != nil {
		t.Fatalf("NotFound on destroy must count as success, got %v", err)
	}
}

func TestRetire_ListFailureIsReportedButValueIsLive(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if _, err := b.Set(ctx, testKey, "v1"); err != nil {
		t.Fatal(err)
	}
	ref, err := b.Set(ctx, testKey, "v2")
	if err != nil {
		t.Fatal(err)
	}
	f.failNext("ListEnabledVersions", status.Error(codes.Unavailable, "backend hiccup"))
	err = b.Retire(ctx, testKey, ref)
	if err == nil || !strings.Contains(err.Error(), "earlier versions were not retired") {
		t.Fatalf("want a retirement error, got %v", err)
	}
	if got, _ := b.GetRef(ctx, ref); got != "v2" {
		t.Fatalf("the new version must be live even when retirement failed, got %q", got)
	}
	if got := f.enabledVersions(b.secretName(testKey)); len(got) != 2 {
		t.Fatalf("v1 should still be enabled after a failed listing, enabled = %v", got)
	}
}

func TestSet_AddVersionFailureIsAnError(t *testing.T) {
	b, f := newTestBackend()
	f.failNext("AddSecretVersion", status.Error(codes.PermissionDenied, "no versions.add"))
	_, err := b.Set(context.Background(), testKey, "v1")
	if err == nil || !strings.Contains(err.Error(), "no versions.add") {
		t.Fatalf("want the permission error surfaced, got %v", err)
	}
}

func TestDelete_MissingIsFine(t *testing.T) {
	b, f := newTestBackend()
	ctx := context.Background()
	if err := b.Delete(ctx, testKey); err != nil {
		t.Fatalf("deleting a missing secret must succeed, got %v", err)
	}
	if _, err := b.Set(ctx, testKey, "v1"); err != nil {
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
	// Two writes, a retirement (DestroySecretVersion), a read by reference.
	if f.count("AddSecretVersion") != 2 || f.count("DestroySecretVersion") != 1 || f.count("AccessSecretVersion") < 2 || f.count("DeleteSecret") < 1 {
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
	// Every delete fails: the probe's own Delete and the deferred cleanup.
	// Both are reported, and the cleanup line names the probe secret so an
	// operator can find the leftover.
	f.failAlways("DeleteSecret", status.Error(codes.PermissionDenied, "no secrets.delete"))
	err := b.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no secrets.delete") {
		t.Fatalf("want the delete error, got %v", err)
	}
	if !strings.Contains(err.Error(), "was not cleaned up") || !strings.Contains(err.Error(), testPrefix+"-") {
		t.Fatalf("want the cleanup failure naming the probe secret, got %v", err)
	}
	if len(f.secrets) != 1 {
		t.Fatalf("the leftover must still exist for the operator to find, have %d", len(f.secrets))
	}
}

func TestClose_ClosesClient(t *testing.T) {
	b, f := newTestBackend()
	if err := b.Close(); err != nil || !f.closed {
		t.Fatalf("Close: err=%v closed=%v", err, f.closed)
	}
}
