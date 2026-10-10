package vault

import "github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault/tokens"

// Encrypt is kept so that existing importers of pkg/vault keep compiling.
//
// Deprecated: use tokens.Encrypt from pkg/vault/tokens.
func Encrypt(key []byte, plaintext []byte) (string, error) {
	return tokens.Encrypt(key, plaintext)
}

// Decrypt is kept so that existing importers of pkg/vault keep compiling.
//
// Deprecated: use tokens.Decrypt from pkg/vault/tokens.
func Decrypt(key []byte, ciphertext string) ([]byte, error) {
	return tokens.Decrypt(key, ciphertext)
}
