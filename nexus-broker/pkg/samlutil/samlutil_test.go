package samlutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCertificate(t *testing.T) {
	pemCert := testCertificatePEM(t)

	cert, err := NormalizeCertificate(pemCert)
	if err != nil {
		t.Fatalf("NormalizeCertificate returned error: %v", err)
	}
	if cert == "" {
		t.Fatal("normalized certificate is empty")
	}
	if strings.Contains(cert, "BEGIN CERTIFICATE") {
		t.Fatal("normalized certificate still contains PEM armor")
	}
}

func TestNormalizeCertificateRejectsInvalidCertificate(t *testing.T) {
	if _, err := NormalizeCertificate("not-a-certificate"); err == nil {
		t.Fatal("expected invalid certificate error")
	}
}

func TestBuildServiceProvider(t *testing.T) {
	sp, err := BuildServiceProvider(ServiceProviderConfig{
		SPEntityID:  "https://broker.example.com/saml/sp",
		ACSURL:      "https://broker.example.com/saml/acs",
		MetadataURL: "https://broker.example.com/saml/metadata/provider-id",
		IDPEntityID: "https://idp.example.com/entity",
		IDPSSOURL:   "https://idp.example.com/sso",
		IDPX509Cert: testCertificatePEM(t),
	})
	if err != nil {
		t.Fatalf("BuildServiceProvider returned error: %v", err)
	}
	if sp.EntityID != "https://broker.example.com/saml/sp" {
		t.Fatalf("EntityID = %q", sp.EntityID)
	}
	if sp.AcsURL.String() != "https://broker.example.com/saml/acs" {
		t.Fatalf("ACS URL = %q", sp.AcsURL.String())
	}
}

func testCertificatePEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Test IdP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
