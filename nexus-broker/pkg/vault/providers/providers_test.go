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
	if err := m.Set(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || got != "v2" {
		t.Fatalf("Get after two Sets = %q, %v; want v2", got, err)
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
