// Package gcp stores provider client secrets in Google Cloud Secret Manager.
//
// One secret per provider, named "<prefix>-<provider uuid>", with automatic
// replication. Set adds a version and destroys the previous enabled versions
// so that only the current secret is billed and readable. The broker's
// service account needs secretmanager.secrets.create on the project and
// Secret Manager admin on secrets under the prefix; see
// deploy/terraform/gcp/secret-backend in the repository root.
package gcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/providers"
)

const managedByLabel = "nexus-broker"

// Backend implements providers.Backend on Secret Manager.
type Backend struct {
	client    *secretmanager.Client
	projectID string
	prefix    string
}

// Config for New.
type Config struct {
	// ProjectID hosting the secrets. Empty means ask the metadata server,
	// which works on Cloud Run, GCE and GKE.
	ProjectID string
	// Prefix for secret IDs. Required.
	Prefix string
}

// New builds a Backend using Application Default Credentials.
func New(ctx context.Context, cfg Config) (*Backend, error) {
	if strings.TrimSpace(cfg.Prefix) == "" {
		return nil, errors.New("gcp secret backend: prefix is required")
	}
	projectID := strings.TrimSpace(cfg.ProjectID)
	if projectID == "" {
		if !metadata.OnGCE() {
			return nil, errors.New("gcp secret backend: GCP_PROJECT_ID is required outside Google Cloud")
		}
		id, err := metadata.ProjectIDWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("gcp secret backend: resolve project from metadata server: %w", err)
		}
		projectID = id
	}

	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcp secret backend: create client: %w", err)
	}
	return &Backend{client: client, projectID: projectID, prefix: cfg.Prefix}, nil
}

// Close releases the underlying gRPC connection.
func (b *Backend) Close() error { return b.client.Close() }

func (b *Backend) Kind() providers.Kind { return providers.KindGCPSecretManager }

// ProjectID reports the project the backend writes to, for startup logs.
func (b *Backend) ProjectID() string { return b.projectID }

func (b *Backend) secretID(key string) string {
	return b.prefix + "-" + key
}

func (b *Backend) secretName(key string) string {
	return fmt.Sprintf("projects/%s/secrets/%s", b.projectID, b.secretID(key))
}

// versionOlder reports whether version resource name a has a smaller numeric
// version id than b. Names end in "/versions/<n>"; unparsable names are
// treated as not older so they are never destroyed by mistake.
func versionOlder(a, b string) bool {
	na, oka := versionNumber(a)
	nb, okb := versionNumber(b)
	return oka && okb && na < nb
}

func versionNumber(name string) (int64, bool) {
	i := strings.LastIndex(name, "/versions/")
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(name[i+len("/versions/"):], 10, 64)
	return n, err == nil
}

func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

func (b *Backend) Get(ctx context.Context, key string) (string, error) {
	resp, err := b.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: b.secretName(key) + "/versions/latest",
	})
	if isNotFound(err) {
		return "", providers.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("gcp secret backend: access %s: %w", b.secretID(key), err)
	}
	return string(resp.GetPayload().GetData()), nil
}

func (b *Backend) Set(ctx context.Context, key, value string) error {
	name := b.secretName(key)

	// Create the container on first write. A race with another writer is
	// resolved by treating AlreadyExists as success.
	_, err := b.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   "projects/" + b.projectID,
		SecretId: b.secretID(key),
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
			Labels: map[string]string{"managed-by": managedByLabel},
		},
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("gcp secret backend: create %s: %w", b.secretID(key), err)
	}

	added, err := b.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  name,
		Payload: &secretmanagerpb.SecretPayload{Data: []byte(value)},
	})
	if err != nil {
		return fmt.Errorf("gcp secret backend: add version to %s: %w", b.secretID(key), err)
	}

	// Retire earlier versions so only the current value is readable and
	// billed. The new version is already live at this point; a failure here
	// is still reported so that callers do not believe the old value is gone.
	it := b.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{
		Parent: name,
		Filter: "state:ENABLED",
	})
	var retireErrs []error
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			retireErrs = append(retireErrs, fmt.Errorf("list versions: %w", err))
			break
		}
		// Retire only versions older than the one just added. A concurrent
		// writer may have added a newer one; destroying it would lose that
		// caller's value.
		if !versionOlder(v.GetName(), added.GetName()) {
			continue
		}
		if _, err := b.client.DestroySecretVersion(ctx, &secretmanagerpb.DestroySecretVersionRequest{Name: v.GetName()}); err != nil {
			retireErrs = append(retireErrs, fmt.Errorf("destroy %s: %w", v.GetName(), err))
		}
	}
	if len(retireErrs) > 0 {
		return fmt.Errorf("gcp secret backend: %s: new version is live but earlier versions were not retired: %w", b.secretID(key), errors.Join(retireErrs...))
	}
	return nil
}

func (b *Backend) Delete(ctx context.Context, key string) error {
	err := b.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: b.secretName(key)})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("gcp secret backend: delete %s: %w", b.secretID(key), err)
	}
	return nil
}

// Ping proves the service account can do everything a registration does:
// it runs Set twice, Get and Delete on a probe secret under the prefix,
// which exercises secret create, version add, version access, version list,
// version destroy and secret delete. The key is a fresh UUID per call so that
// replicas starting together cannot interfere with each other's probe, and
// the probe is deleted on every exit path. The version lives for well under
// a second, so the cost is nil. Any failure, such as a missing create binding,
// PermissionDenied on the prefix, or the API being disabled, is reported at
// startup rather than on the first registration.
func (b *Backend) Ping(ctx context.Context) error {
	key := uuid.NewString()
	defer func() {
		// Best-effort cleanup under its own short deadline, so a probe that
		// timed out does not leave startup hanging in the delete.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = b.Delete(cleanupCtx, key)
	}()

	// Written twice so the second Set has to retire the first version,
	// which exercises DestroySecretVersion as a real rotation would.
	if err := b.Set(ctx, key, "startup-probe-1"); err != nil {
		return fmt.Errorf("gcp secret backend: project %s, prefix %s: %w", b.projectID, b.prefix, err)
	}
	if err := b.Set(ctx, key, "startup-probe-2"); err != nil {
		return fmt.Errorf("gcp secret backend: project %s, prefix %s: %w", b.projectID, b.prefix, err)
	}
	if _, err := b.Get(ctx, key); err != nil {
		return fmt.Errorf("gcp secret backend: project %s, prefix %s: %w", b.projectID, b.prefix, err)
	}
	if err := b.Delete(ctx, key); err != nil {
		return fmt.Errorf("gcp secret backend: project %s, prefix %s: %w", b.projectID, b.prefix, err)
	}
	return nil
}
