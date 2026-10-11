package providers

import (
	"context"
	"errors"
	"testing"
)

func TestParseKind(t *testing.T) {
	cases := map[string]struct {
		want Kind
		ok   bool
	}{
		"":                   {KindInternal, true},
		"internal":           {KindInternal, true},
		"gcp-secret-manager": {KindGCPSecretManager, true},
		"hashicorp-vault":    {"", false},
	}
	for raw, c := range cases {
		got, err := ParseKind(raw)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("ParseKind(%q) = %q, %v; want %q", raw, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("ParseKind(%q) accepted an unknown backend", raw)
		}
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	if _, err := m.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty backend: want ErrNotFound, got %v", err)
	}
	ref1, err := m.Set(ctx, "k", "v1")
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := m.Set(ctx, "k", "v2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || got != "v2" {
		t.Fatalf("Get after two Sets = %q, %v; want v2", got, err)
	}
	// Both versions readable by reference until Retire; then only the kept one.
	if v, err := m.GetRef(ctx, ref1); err != nil || v != "v1" {
		t.Fatalf("GetRef(ref1) = %q, %v", v, err)
	}
	if err := m.Retire(ctx, "k", ref2); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetRef(ctx, ref1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired version must be ErrNotFound, got %v", err)
	}
	if v, err := m.GetRef(ctx, ref2); err != nil || v != "v2" {
		t.Fatalf("GetRef(ref2) = %q, %v", v, err)
	}
	if m.LiveVersions("k") != 1 {
		t.Fatalf("LiveVersions = %d, want 1", m.LiveVersions("k"))
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("deleting a missing key must not error, got %v", err)
	}
	if m.Len() != 0 {
		t.Fatalf("Len after delete = %d, want 0", m.Len())
	}
	m.PingErr = errors.New("down")
	if err := m.Ping(ctx); err == nil {
		t.Fatal("Ping should surface PingErr")
	}
}
