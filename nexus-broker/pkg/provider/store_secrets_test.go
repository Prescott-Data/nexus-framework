package provider

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/providers"
)

// failingBackend rejects every write so the store's rollback path can be
// exercised without a real backend.
type failingBackend struct {
	providers.Memory
	deleted []string
}

func (f *failingBackend) Set(context.Context, string, string) (string, error) {
	return "", errors.New("backend unavailable")
}

func (f *failingBackend) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

const (
	testProviderID = "a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0"
	testSecret     = "test-client-secret"
)

var profileColumns = []string{
	"id", "name", "client_id", "client_secret", "auth_url", "token_url", "issuer",
	"enable_discovery", "scopes", "auth_type", "auth_header", "api_base_url",
	"user_info_endpoint", "params", "description", "category", "last_health_check_at",
	"health_status", "health_message", "saml_idp_entity_id", "saml_idp_sso_url",
	"saml_idp_x509_cert", "saml_sp_entity_id", "client_secret_ref", "client_secret_fingerprint",
}

// profileRow is a provider_profiles row whose client_secret column is NULL,
// the shape every row has once an external backend is active. ref is the
// stored version reference, or nil for a row written before references.
func profileRow(authType string, ref interface{}) *sqlmock.Rows {
	return sqlmock.NewRows(profileColumns).AddRow(
		testProviderID, "acme", "client-id", nil, "https://acme/auth", "https://acme/token", nil,
		false, "{}", authType, "", "", "", nil, "", "", nil, "unknown", nil, nil, nil, nil, nil, ref, nil,
	)
}

func newStoreWithBackend(t *testing.T, backend providers.Backend) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewStore(sqlx.NewDb(db, "sqlmock"), WithSecretBackend(backend)), mock
}

func expectLock(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(hashtext\(\$1\)\)`).WithArgs(testProviderID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestRegisterProfile_ExternalBackend_RowHoldsNullAndRef(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("acme").WillReturnError(sql.ErrNoRows)
	// The secret is written first; the row carries NULL and the version ref,
	// inside a transaction that commits only after the insert succeeds.
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO provider_profiles\s+\(id, client_secret_ref, name, client_id, client_secret,`).
		WithArgs(
			"acme", "client-id", nil, // client_secret must be NULL in the row
			"https://acme/auth", "https://acme/token", nil, false, pq.Array([]string{}),
			"oauth2", "", "", "", sqlmock.AnyArg(), "", "", nil, nil, nil, nil,
			SecretFingerprint(testSecret), // fingerprint, returned by the API
			sqlmock.AnyArg(),              // generated id
			sqlmock.AnyArg(),              // version reference
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))
	mock.ExpectCommit()

	p, err := store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"` + testSecret + `","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 1, backend.Len(), "exactly one secret must be written, keyed by the generated id")
	require.NotNil(t, p.ClientSecretRef)
	got, err := backend.GetRef(context.Background(), *p.ClientSecretRef)
	require.NoError(t, err)
	assert.Equal(t, testSecret, got, "the returned profile references the version just written")
}

func TestRegisterProfile_ExternalBackend_WriteFailureWritesNoRow(t *testing.T) {
	backend := &failingBackend{}
	store, mock := newStoreWithBackend(t, backend)
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("acme").WillReturnError(sql.ErrNoRows)
	// No Begin, no INSERT: the secret write fails before the row is touched.

	_, err := store.RegisterProfile(`{"name":"acme","client_id":"client-id","client_secret":"s","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSecretBackend)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterProfile_ExternalBackend_InsertFailureRemovesSecret(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO provider_profiles`).WillReturnError(errors.New("unique_violation"))
	mock.ExpectRollback()

	_, err := store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"s","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.Error(t, err)
	assert.Equal(t, 0, backend.Len(), "the secret written before the failed insert must be removed")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterProfile_ExternalBackend_AmbiguousCommitKeepsSecret(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO provider_profiles`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))
	mock.ExpectCommit().WillReturnError(errors.New("connection reset during commit"))

	_, err := store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"s","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown outcome")
	assert.Equal(t, 1, backend.Len(), "the secret is kept whenever the commit outcome is unknown")
}

func TestRegisterProfile_RejectsWhitespaceSecret(t *testing.T) {
	store, _ := newStoreWithBackend(t, providers.NewMemory())
	_, err := store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"   ","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_secret: missing required field")
}

func TestGetProfile_IsMetadataOnly(t *testing.T) {
	// Reads never touch the backend: a provider whose secret is unreachable
	// still lists, and an admin GET costs no secret access.
	backend := &failingBackend{}
	store, mock := newStoreWithBackend(t, backend)
	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).WillReturnRows(profileRow("oauth2", "mem:x:1"))

	p, err := store.GetProfile(uuid.MustParse(testProviderID))
	require.NoError(t, err)
	assert.Nil(t, p.ClientSecret)
	assert.False(t, p.ClientSecretMissing, "the read does not resolve, so it cannot know")
	require.NotNil(t, p.ClientSecretRef)
}

func TestResolveClientSecret_ByReference(t *testing.T) {
	backend := providers.NewMemory()
	ctx := context.Background()
	ref1, _ := backend.Set(ctx, testProviderID, "old")
	ref2, _ := backend.Set(ctx, testProviderID, "new")
	store, _ := newStoreWithBackend(t, backend)

	// A row that still references the previous version reads that version,
	// not whatever is newest: metadata and secret stay paired.
	p := &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &ref1}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	require.NotNil(t, p.ClientSecret)
	assert.Equal(t, "old", *p.ClientSecret)

	p = &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &ref2}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.Equal(t, "new", *p.ClientSecret)
}

func TestResolveClientSecret_LegacyRowBackfillsReferenceUnderLock(t *testing.T) {
	backend := providers.NewMemory()
	ctx := context.Background()
	_, _ = backend.Set(ctx, testProviderID, "old")
	ref2, _ := backend.Set(ctx, testProviderID, "new")
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	// Lock, re-read (still no reference), backfill the current version's reference.
	expectLock(mock)
	mock.ExpectQuery(`SELECT client_secret_ref FROM provider_profiles WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"client_secret_ref"}).AddRow(nil))
	mock.ExpectExec(`UPDATE provider_profiles SET client_secret_ref = \$1 WHERE id = \$2 AND client_secret_ref IS NULL`).
		WithArgs(ref2, id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	p := &Profile{ID: id, AuthType: "oauth2"} // no ref: written before 0.6.2
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, "new", *p.ClientSecret)
	require.NotNil(t, p.ClientSecretRef)
	assert.Equal(t, ref2, *p.ClientSecretRef, "the row now carries the reference it will read by from here on")
}

func TestResolveClientSecret_LegacyRowRotatedMeanwhileUsesTheRotationsPair(t *testing.T) {
	backend := providers.NewMemory()
	ctx := context.Background()
	_, _ = backend.Set(ctx, testProviderID, "old")
	ref2, _ := backend.Set(ctx, testProviderID, "new")
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	// By the time the lock is held a rotation has written a reference: the
	// re-read row, metadata included, is what gets used; no backfill.
	expectLock(mock)
	mock.ExpectQuery(`SELECT client_secret_ref FROM provider_profiles WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"client_secret_ref"}).AddRow(ref2))
	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).WillReturnRows(profileRow("oauth2", ref2))
	mock.ExpectCommit()
	mock.ExpectExec(`UPDATE provider_profiles SET client_secret_fingerprint = \$1 WHERE id = \$2 AND client_secret_fingerprint IS NULL`).
		WithArgs(SecretFingerprint("new"), id).WillReturnResult(sqlmock.NewResult(0, 1))

	p := &Profile{ID: id, AuthType: "oauth2", Name: "stale"}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, "new", *p.ClientSecret)
	assert.Equal(t, "acme", p.Name, "metadata comes from the re-read row")
	assert.Equal(t, ref2, *p.ClientSecretRef)
}

func TestResolveClientSecret_MissingRetiredBlankAndNonOAuth(t *testing.T) {
	backend := providers.NewMemory()
	ctx := context.Background()
	store, mock := newStoreWithBackend(t, backend)

	missingRef := providers.MemoryRef(testProviderID, 99)
	p := &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &missingRef}
	mock.ExpectQuery(`SELECT client_secret_ref FROM provider_profiles`).
		WillReturnRows(sqlmock.NewRows([]string{"client_secret_ref"}).AddRow(missingRef)) // unchanged: truly gone
	require.NoError(t, store.ResolveClientSecret(ctx, p), "a missing secret must not fail the call")
	assert.True(t, p.ClientSecretMissing)
	_, err := p.RequireClientSecret()
	assert.ErrorIs(t, err, ErrClientSecretMissing)

	ref1, _ := backend.Set(ctx, testProviderID, "v1")
	ref2, _ := backend.Set(ctx, testProviderID, "v2")
	require.NoError(t, backend.Retire(ctx, testProviderID, ref2))
	p = &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &ref1}
	mock.ExpectQuery(`SELECT client_secret_ref FROM provider_profiles`).
		WillReturnRows(sqlmock.NewRows([]string{"client_secret_ref"}).AddRow(ref1)) // row still says ref1: retired out of band
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.True(t, p.ClientSecretMissing, "a retired reference the row still points at fails closed")

	refBlank, _ := backend.Set(ctx, testProviderID, "   ")
	p = &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &refBlank}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.True(t, p.ClientSecretMissing, "a blank value fails closed")

	p = &Profile{ID: uuid.MustParse(testProviderID), AuthType: "api_key", ClientSecretRef: &ref2}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.Nil(t, p.ClientSecret, "static auth types have no client secret to resolve")
	assert.False(t, p.ClientSecretMissing)
}

func TestResolveClientSecret_InternalBackend(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	store := NewStore(sqlx.NewDb(db, "sqlmock"))
	ctx := context.Background()

	secret := "s"
	p := &Profile{AuthType: "oauth2", ClientSecret: &secret}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.Equal(t, "s", *p.ClientSecret)

	// A NULL column under the internal backend can only be a row written
	// while an external backend was active: fail closed.
	p = &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2"}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.True(t, p.ClientSecretMissing)
}

func TestRequireClientSecret(t *testing.T) {
	secret := "s"
	got, err := (&Profile{ClientSecret: &secret}).RequireClientSecret()
	require.NoError(t, err)
	assert.Equal(t, "s", got)
	_, err = (&Profile{ClientSecretMissing: true}).RequireClientSecret()
	assert.ErrorIs(t, err, ErrClientSecretMissing)
}

func TestPatchProfile_ExternalBackend_RotatesAtomicallyThenRetires(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	_, _ = backend.Set(context.Background(), testProviderID, "old")

	expectLock(mock)
	// Column NULL, reference set, both in the one statement.
	mock.ExpectExec(`UPDATE provider_profiles SET client_secret = \$1, client_secret_fingerprint = \$2, client_secret_ref = \$3, updated_at = NOW\(\) WHERE id = \$4`).
		WithArgs(nil, SecretFingerprint("rotated"), providers.MemoryRef(testProviderID, 2), id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, store.PatchProfile(id, map[string]interface{}{"client_secret": "rotated"}))
	assert.NoError(t, mock.ExpectationsWereMet())
	got, err := backend.GetRef(context.Background(), providers.MemoryRef(testProviderID, 2))
	require.NoError(t, err)
	assert.Equal(t, "rotated", got)
	assert.Equal(t, 1, backend.LiveVersions(testProviderID), "the old version is retired after the row commits")
}

func TestPatchProfile_ExternalBackend_UnknownProviderDoesNotOrphanSecret(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	expectLock(mock)
	mock.ExpectExec(`UPDATE provider_profiles SET client_secret = \$1, client_secret_fingerprint = \$2, client_secret_ref = \$3, updated_at = NOW\(\) WHERE id = \$4`).
		WithArgs(nil, sqlmock.AnyArg(), sqlmock.AnyArg(), id).WillReturnResult(sqlmock.NewResult(0, 0)) // no active row
	mock.ExpectRollback()

	err := store.PatchProfile(id, map[string]interface{}{"client_secret": "rotated"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderNotFound)
	assert.Equal(t, 0, backend.Len(), "the secret written before the UPDATE must be removed again")
}

func TestUpdateProfile_ExternalBackend_RotatesAtomicallyThenRetires(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	_, _ = backend.Set(context.Background(), testProviderID, "old")
	secret := "updated"

	expectLock(mock)
	// $3 = rotating, $21 = the new reference, in the same statement as the metadata.
	mock.ExpectExec(`client_secret = CASE WHEN \$3::boolean THEN NULL ELSE client_secret END, client_secret_ref = CASE WHEN \$3::boolean THEN \$22 ELSE client_secret_ref END`).
		WithArgs("acme", "client-id", true, nil, nil, nil, false, pq.Array([]string(nil)), "oauth2", "", "", "", nil, "", "", nil, nil, nil, nil, id, SecretFingerprint(secret), providers.MemoryRef(testProviderID, 2)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cid := "client-id"
	require.NoError(t, store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &secret}))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 1, backend.LiveVersions(testProviderID))
}

func TestUpdateProfile_ExternalBackend_OmittedSecretPreservesColumnAndRef(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	// No secret in the PUT: $3 = false, ref NULL; the CASE keeps both columns.
	mock.ExpectExec(`client_secret_ref = CASE WHEN \$3::boolean THEN \$22 ELSE client_secret_ref END`).
		WithArgs("acme", "client-id", false, nil, nil, nil, false, pq.Array([]string(nil)), "oauth2", "", "", "", nil, "", "", nil, nil, nil, nil, id, nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	cid := "client-id"
	require.NoError(t, store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid}))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 0, backend.Len(), "nothing is written to the backend when the secret is omitted")
}

func TestRotate_ExternalBackend_RejectsEmptySecret(t *testing.T) {
	backend := providers.NewMemory()
	store, _ := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	empty := "  "

	err := store.PatchProfile(id, map[string]interface{}{"client_secret": ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidProfile)

	cid := "client-id"
	err = store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &empty})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidProfile)
	assert.Equal(t, 0, backend.Len(), "an empty secret must never reach the backend")
}

func TestDeleteProfile_ExternalBackend_RemovesSecret(t *testing.T) {
	backend := providers.NewMemory()
	_, _ = backend.Set(context.Background(), testProviderID, testSecret)
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	mock.ExpectExec(`UPDATE provider_profiles SET deleted_at = NOW\(\), client_secret = NULL WHERE id = \$1`).
		WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.DeleteProfile(id))
	assert.Equal(t, 0, backend.Len())
}

func TestDeleteProfileByName_ExternalBackend_RemovesEverySecret(t *testing.T) {
	backend := providers.NewMemory()
	_, _ = backend.Set(context.Background(), testProviderID, testSecret)
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`UPDATE provider_profiles SET deleted_at = NOW\(\), client_secret = NULL WHERE name = \$1 AND deleted_at IS NULL RETURNING id`).
		WithArgs("acme").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))
	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1 AND deleted_at IS NOT NULL`).
		WithArgs("acme").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID)) // same row now historical; deduped

	n, err := store.DeleteProfileByName("acme")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, 0, backend.Len())
}

func TestInternalBackend_IsUnchanged(t *testing.T) {
	// No backend configured: the column carries the secret exactly as before.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	store := NewStore(sqlx.NewDb(db, "sqlmock"))

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs("acme", "client-id", testSecret, "https://acme/auth", "https://acme/token", nil, false, pq.Array([]string{}),
			"oauth2", "", "", "", sqlmock.AnyArg(), "", "", nil, nil, nil, nil, SecretFingerprint(testSecret)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))

	_, err = store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"` + testSecret + `","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveClientSecret_RetiredReferenceReloadsTheRow(t *testing.T) {
	// A reader loaded the row just before a rotation committed and retired
	// the version it references. Resolving must re-read the row and use the
	// rotation's pair, not fail with a missing secret.
	backend := providers.NewMemory()
	ctx := context.Background()
	ref1, _ := backend.Set(ctx, testProviderID, "old")
	ref2, _ := backend.Set(ctx, testProviderID, "new")
	require.NoError(t, backend.Retire(ctx, testProviderID, ref2))
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`SELECT client_secret_ref FROM provider_profiles WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(uuid.MustParse(testProviderID)).WillReturnRows(sqlmock.NewRows([]string{"client_secret_ref"}).AddRow(ref2))
	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).WillReturnRows(profileRow("oauth2", ref2))
	mock.ExpectExec(`UPDATE provider_profiles SET client_secret_fingerprint = \$1 WHERE id = \$2 AND client_secret_fingerprint IS NULL`).
		WithArgs(SecretFingerprint("new"), uuid.MustParse(testProviderID)).WillReturnResult(sqlmock.NewResult(0, 1))

	p := &Profile{ID: uuid.MustParse(testProviderID), AuthType: "oauth2", ClientSecretRef: &ref1}
	require.NoError(t, store.ResolveClientSecret(ctx, p))
	assert.NoError(t, mock.ExpectationsWereMet())
	require.NotNil(t, p.ClientSecret)
	assert.Equal(t, "new", *p.ClientSecret)
	assert.Equal(t, ref2, *p.ClientSecretRef, "the profile now carries the rotation's reference and metadata")
	assert.False(t, p.ClientSecretMissing)
}

func TestUpdateProfile_ExternalBackend_FailedUpdateDiscardsTheWrittenVersion(t *testing.T) {
	// The row update fails for sure (not an ambiguous commit): the version
	// written for it must be discarded so a pre-0.6.2 row, which reads the
	// backend's current version, cannot pick it up.
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	_, _ = backend.Set(context.Background(), testProviderID, "old")
	secret := "rejected"

	expectLock(mock)
	mock.ExpectExec(`client_secret_ref = CASE WHEN \$3::boolean THEN \$22 ELSE client_secret_ref END`).
		WillReturnError(errors.New("deadlock detected"))
	mock.ExpectRollback()

	cid := "client-id"
	err := store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &secret})
	require.Error(t, err)
	got, _, gErr := backend.Current(context.Background(), testProviderID)
	require.NoError(t, gErr)
	assert.Equal(t, "old", got, "the current version must still be the old one")
	assert.Equal(t, 1, backend.LiveVersions(testProviderID))
}

func TestUpdateProfile_ExternalBackend_AmbiguousCommitKeepsTheWrittenVersion(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	_, _ = backend.Set(context.Background(), testProviderID, "old")
	secret := "maybe"

	expectLock(mock)
	mock.ExpectExec(`client_secret_ref = CASE WHEN \$3::boolean THEN \$22 ELSE client_secret_ref END`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("connection reset during commit"))

	cid := "client-id"
	err := store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &secret})
	require.Error(t, err)
	assert.Equal(t, 2, backend.LiveVersions(testProviderID), "an unknown commit outcome keeps the version the row may reference")
}
