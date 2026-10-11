package gcp

import (
	"context"
	"errors"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/iterator"
)

// secretClient is the slice of the Secret Manager API the backend uses.
// Backend talks only to this interface, so the lifecycle logic (create or
// reuse, add a version, retire earlier ones, delete, probe) is tested with
// the in-memory fake in fake_test.go; the real client is exercised by the
// credentialed round-trip test.
type secretClient interface {
	AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error)
	CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error)
	AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error)
	// ListEnabledVersions returns every ENABLED version of the secret.
	ListEnabledVersions(ctx context.Context, secretName string) ([]*secretmanagerpb.SecretVersion, error)
	DestroySecretVersion(ctx context.Context, req *secretmanagerpb.DestroySecretVersionRequest) (*secretmanagerpb.SecretVersion, error)
	DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error
	Close() error
}

// gapiClient adapts the generated client to secretClient.
type gapiClient struct {
	c *secretmanager.Client
}

func (g gapiClient) AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	return g.c.AccessSecretVersion(ctx, req)
}

func (g gapiClient) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	return g.c.CreateSecret(ctx, req)
}

func (g gapiClient) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return g.c.AddSecretVersion(ctx, req)
}

func (g gapiClient) ListEnabledVersions(ctx context.Context, secretName string) ([]*secretmanagerpb.SecretVersion, error) {
	it := g.c.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: secretName, Filter: "state:ENABLED"})
	var out []*secretmanagerpb.SecretVersion
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

func (g gapiClient) DestroySecretVersion(ctx context.Context, req *secretmanagerpb.DestroySecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return g.c.DestroySecretVersion(ctx, req)
}

func (g gapiClient) DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error {
	return g.c.DeleteSecret(ctx, req)
}

func (g gapiClient) Close() error { return g.c.Close() }
