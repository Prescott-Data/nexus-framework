// Package vault groups the broker's two secret-handling concerns.
//
//   - tokens: AES-GCM encryption of captured credentials and OAuth tokens
//     before they are written to Postgres. The database only ever holds
//     ciphertext; the key comes from ENCRYPTION_KEY.
//   - providers: where provider client secrets live. By default they stay in
//     the provider_profiles row (the "internal" backend). With an external
//     backend such as GCP Secret Manager the row holds NULL and the secret is
//     fetched from the backend when an OAuth flow needs it.
package vault
