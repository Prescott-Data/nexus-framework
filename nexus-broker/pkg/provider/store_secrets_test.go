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

func (f *failingBackend) Set(context.Context, string, string) error {
	return errors.New("backend unavailable")
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
	"saml_idp_x509_cert", "saml_sp_entity_id",
}

// profileRow is a provider_profiles row whose client_secret column is NULL,
// the shape every row has once an external backend is active.
func profileRow(authType string) *sqlmock.Rows {
	return sqlmock.NewRows(profileColumns).AddRow(
		testProviderID, "acme", "client-id", nil, "https://acme/auth", "https://acme/token", nil,
		false, "{}", authType, "", "", "", nil, "", "", nil, "unknown", nil, nil, nil, nil, nil,
	)
}

func newStoreWithBackend(t *testing.T, backend providers.Backend) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewStore(sqlx.NewDb(db, "sqlmock"), WithSecretBackend(backend)), mock
}

func TestRegisterProfile_ExternalBackend_RowHoldsNull(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("acme").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WithArgs(
			"acme", "client-id", nil, // client_secret must be NULL in the row
			"https://acme/auth", "https://acme/token", nil, false, pq.Array([]string{}),
			"oauth2", "", "", "", sqlmock.AnyArg(), "", "", nil, nil, nil, nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))

	p, err := store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"` + testSecret + `","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())

	got, err := backend.Get(context.Background(), testProviderID)
	require.NoError(t, err)
	assert.Equal(t, testSecret, got, "secret must be written to the backend keyed by provider id")
	// The returned profile still carries the secret for the caller that just supplied it.
	require.NotNil(t, p.ClientSecret)
	assert.Equal(t, testSecret, *p.ClientSecret)
}

func TestRegisterProfile_ExternalBackend_WriteFailureRollsBackRow(t *testing.T) {
	backend := &failingBackend{}
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("acme").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO provider_profiles`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))
	mock.ExpectExec(`DELETE FROM provider_profiles WHERE id = \$1`).
		WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))

	_, err := store.RegisterProfile(`{"name":"acme","client_id":"client-id","client_secret":"s","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend unavailable")
	assert.NoError(t, mock.ExpectationsWereMet(), "the half-registered row must be removed")
	assert.Equal(t, []string{testProviderID}, backend.deleted, "a partially written secret must be removed too")
}

func TestGetProfile_ExternalBackend_ResolvesSecret(t *testing.T) {
	backend := providers.NewMemory()
	require.NoError(t, backend.Set(context.Background(), testProviderID, testSecret))
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).
		WithArgs(uuid.MustParse(testProviderID)).WillReturnRows(profileRow("oauth2"))

	p, err := store.GetProfile(uuid.MustParse(testProviderID))
	require.NoError(t, err)
	require.NotNil(t, p.ClientSecret)
	assert.Equal(t, testSecret, *p.ClientSecret)
}

func TestGetProfile_ExternalBackend_MissingSecretStaysNil(t *testing.T) {
	store, mock := newStoreWithBackend(t, providers.NewMemory())
	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).WillReturnRows(profileRow("oauth2"))

	p, err := store.GetProfile(uuid.MustParse(testProviderID))
	require.NoError(t, err, "a missing secret must not fail the read")
	assert.Nil(t, p.ClientSecret)
	assert.True(t, p.ClientSecretMissing, "the read must flag the missing secret")
	_, err = p.RequireClientSecret()
	assert.ErrorIs(t, err, ErrClientSecretMissing, "execution paths must refuse rather than send an empty secret")
}

func TestRequireClientSecret_InternalBackendUnchanged(t *testing.T) {
	secret := "s"
	got, err := (&Profile{ClientSecret: &secret}).RequireClientSecret()
	require.NoError(t, err)
	assert.Equal(t, "s", got)
	got, err = (&Profile{}).RequireClientSecret()
	require.NoError(t, err, "nil without the missing flag is the pre-existing public-client behaviour")
	assert.Equal(t, "", got)
}

func TestGetProfile_ExternalBackend_SkipsNonOAuthTypes(t *testing.T) {
	backend := providers.NewMemory()
	require.NoError(t, backend.Set(context.Background(), testProviderID, "should-not-be-read"))
	store, mock := newStoreWithBackend(t, backend)
	mock.ExpectQuery(`FROM provider_profiles WHERE id = \$1`).WillReturnRows(profileRow("api_key"))

	p, err := store.GetProfile(uuid.MustParse(testProviderID))
	require.NoError(t, err)
	assert.Nil(t, p.ClientSecret, "static auth types have no client secret to resolve")
}

func TestPatchProfile_ExternalBackend_WritesBackendAndNullsColumn(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	mock.ExpectExec(`UPDATE provider_profiles SET client_secret = \$1, updated_at = NOW\(\) WHERE id = \$2`).
		WithArgs(nil, id).WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.PatchProfile(id, map[string]interface{}{"client_secret": "rotated"}))
	assert.NoError(t, mock.ExpectationsWereMet())
	got, err := backend.Get(context.Background(), testProviderID)
	require.NoError(t, err)
	assert.Equal(t, "rotated", got)
}

func TestUpdateProfile_ExternalBackend(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	secret := "updated"

	// A rotation clears the column ($3 = true) after the backend write.
	mock.ExpectExec(`client_secret = CASE WHEN \$3::boolean THEN NULL ELSE client_secret END`).
		WithArgs("acme", "client-id", true, nil, nil, nil, false, pq.Array([]string(nil)), "oauth2", "", "", "", nil, "", "", nil, nil, nil, nil, id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	cid := "client-id"
	require.NoError(t, store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &secret}))
	assert.NoError(t, mock.ExpectationsWereMet())
	got, err := backend.Get(context.Background(), testProviderID)
	require.NoError(t, err)
	assert.Equal(t, secret, got)
}

func TestUpdateProfile_ExternalBackend_OmittedSecretPreservesColumn(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	// No secret in the PUT: $3 = false, so the column is left untouched and a
	// legacy row that still holds its secret in Postgres keeps it.
	mock.ExpectExec(`client_secret = CASE WHEN \$3::boolean THEN NULL ELSE client_secret END`).
		WithArgs("acme", "client-id", false, nil, nil, nil, false, pq.Array([]string(nil)), "oauth2", "", "", "", nil, "", "", nil, nil, nil, nil, id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	cid := "client-id"
	require.NoError(t, store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid}))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 0, backend.Len(), "nothing is written to the backend when the secret is omitted")
}

func TestPatchProfile_ExternalBackend_UnknownProviderDoesNotOrphanSecret(t *testing.T) {
	backend := providers.NewMemory()
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	mock.ExpectExec(`UPDATE provider_profiles SET client_secret = \$1, updated_at = NOW\(\) WHERE id = \$2`).
		WithArgs(nil, id).WillReturnResult(sqlmock.NewResult(0, 0)) // no active row

	err := store.PatchProfile(id, map[string]interface{}{"client_secret": "rotated"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, 0, backend.Len(), "the secret written before the UPDATE must be removed again")
}

func TestDeleteProfile_ExternalBackend_RemovesSecret(t *testing.T) {
	backend := providers.NewMemory()
	require.NoError(t, backend.Set(context.Background(), testProviderID, testSecret))
	store, mock := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)

	mock.ExpectExec(`UPDATE provider_profiles SET deleted_at = NOW\(\) WHERE id = \$1`).
		WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.DeleteProfile(id))
	assert.Equal(t, 0, backend.Len())
}

func TestDeleteProfileByName_ExternalBackend_RemovesEverySecret(t *testing.T) {
	backend := providers.NewMemory()
	require.NoError(t, backend.Set(context.Background(), testProviderID, testSecret))
	store, mock := newStoreWithBackend(t, backend)

	mock.ExpectQuery(`SELECT id FROM provider_profiles WHERE name = \$1`).
		WithArgs("acme").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))
	mock.ExpectExec(`UPDATE provider_profiles SET deleted_at = NOW\(\) WHERE name = \$1`).
		WithArgs("acme").WillReturnResult(sqlmock.NewResult(0, 1))

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
			"oauth2", "", "", "", sqlmock.AnyArg(), "", "", nil, nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testProviderID))

	_, err = store.RegisterProfile(`{"name":"acme","auth_type":"oauth2","client_id":"client-id","client_secret":"` + testSecret + `","auth_url":"https://acme/auth","token_url":"https://acme/token"}`)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRotate_ExternalBackend_RejectsEmptySecret(t *testing.T) {
	backend := providers.NewMemory()
	store, _ := newStoreWithBackend(t, backend)
	id := uuid.MustParse(testProviderID)
	empty := "  "

	err := store.PatchProfile(id, map[string]interface{}{"client_secret": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")

	cid := "client-id"
	err = store.UpdateProfile(&Profile{ID: id, Name: "acme", AuthType: "oauth2", ClientID: &cid, ClientSecret: &empty})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
	assert.Equal(t, 0, backend.Len(), "an empty secret must never reach the backend")
}
