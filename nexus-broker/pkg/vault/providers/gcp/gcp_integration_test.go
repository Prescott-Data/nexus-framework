package gcp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/providers"
)

// Runs only when NEXUS_TEST_GCP_PROJECT names a project the Application
// Default Credentials can write secrets to. CI has no credentials and skips.
func TestBackend_RoundTrip(t *testing.T) {
	project := os.Getenv("NEXUS_TEST_GCP_PROJECT")
	if project == "" {
		t.Skip("NEXUS_TEST_GCP_PROJECT not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b, err := New(ctx, Config{ProjectID: project, Prefix: "nexus-providers-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	key := uuid.NewString()
	t.Cleanup(func() { _ = b.Delete(context.Background(), key) })

	if _, err := b.Get(ctx, key); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("Get before Set: want ErrNotFound, got %v", err)
	}
	ref1, err := b.Set(ctx, key, "first")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	ref2, err := b.Set(ctx, key, "second")
	if err != nil {
		t.Fatalf("Set (rotate): %v", err)
	}
	if v, err := b.GetRef(ctx, ref1); err != nil || v != "first" {
		t.Fatalf("GetRef(ref1) before retire = %q, %v; want first", v, err)
	}
	if err := b.Retire(ctx, key, ref2); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := b.GetRef(ctx, ref1); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("GetRef(ref1) after retire: want ErrNotFound, got %v", err)
	}
	got, err := b.Get(ctx, key)
	if err != nil || got != "second" {
		t.Fatalf("Get after rotate = %q, %v; want second", got, err)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of missing secret must succeed, got %v", err)
	}
	if _, err := b.Get(ctx, key); !errors.Is(err, providers.ErrNotFound) {
		t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
	}
}
