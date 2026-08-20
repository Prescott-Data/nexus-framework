package samlutil

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"

	"github.com/crewjam/saml"
)

type ServiceProviderConfig struct {
	SPEntityID  string
	ACSURL      string
	MetadataURL string
	IDPEntityID string
	IDPSSOURL   string
	IDPX509Cert string
}

func BuildServiceProvider(cfg ServiceProviderConfig) (*saml.ServiceProvider, error) {
	if strings.TrimSpace(cfg.SPEntityID) == "" {
		return nil, fmt.Errorf("saml_sp_entity_id is required")
	}
	if strings.TrimSpace(cfg.IDPEntityID) == "" {
		return nil, fmt.Errorf("saml_idp_entity_id is required")
	}

	acsURL, err := parseHTTPSURL("acs_url", cfg.ACSURL)
	if err != nil {
		return nil, err
	}
	metadataURL, err := parseHTTPSURL("metadata_url", cfg.MetadataURL)
	if err != nil {
		return nil, err
	}
	idpSSOURL, err := parseHTTPSURL("saml_idp_sso_url", cfg.IDPSSOURL)
	if err != nil {
		return nil, err
	}
	cert, err := NormalizeCertificate(cfg.IDPX509Cert)
	if err != nil {
		return nil, err
	}

	idpMetadata := &saml.EntityDescriptor{
		EntityID: cfg.IDPEntityID,
		IDPSSODescriptors: []saml.IDPSSODescriptor{
			{
				SingleSignOnServices: []saml.Endpoint{
					{Binding: saml.HTTPRedirectBinding, Location: idpSSOURL.String()},
					{Binding: saml.HTTPPostBinding, Location: idpSSOURL.String()},
				},
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						KeyDescriptors: []saml.KeyDescriptor{
							{
								Use:     "signing",
								KeyInfo: saml.KeyInfo{X509Data: saml.X509Data{X509Certificates: []saml.X509Certificate{{Data: cert}}}},
							},
						},
					},
				},
			},
		},
	}

	return &saml.ServiceProvider{
		EntityID:    cfg.SPEntityID,
		AcsURL:      *acsURL,
		MetadataURL: *metadataURL,
		IDPMetadata: idpMetadata,
	}, nil
}

func NormalizeCertificate(raw string) (string, error) {
	cert := strings.TrimSpace(raw)
	if cert == "" {
		return "", fmt.Errorf("saml_idp_x509_cert is required")
	}

	var der []byte
	if block, _ := pem.Decode([]byte(cert)); block != nil {
		if block.Type != "CERTIFICATE" {
			return "", fmt.Errorf("saml_idp_x509_cert must contain a CERTIFICATE PEM block")
		}
		der = block.Bytes
	} else {
		compact := strings.NewReplacer("\r", "", "\n", "", "\t", "", " ", "").Replace(cert)
		decoded, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return "", fmt.Errorf("saml_idp_x509_cert must be PEM or base64 DER: %w", err)
		}
		der = decoded
	}

	if _, err := x509.ParseCertificate(der); err != nil {
		return "", fmt.Errorf("saml_idp_x509_cert is not a valid X.509 certificate: %w", err)
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

func ValidateProviderConfig(idpEntityID, idpSSOURL, idpCert, spEntityID string) error {
	_, err := BuildServiceProvider(ServiceProviderConfig{
		SPEntityID:  spEntityID,
		ACSURL:      "https://nexus.invalid/saml/acs",
		MetadataURL: "https://nexus.invalid/saml/metadata",
		IDPEntityID: idpEntityID,
		IDPSSOURL:   idpSSOURL,
		IDPX509Cert: idpCert,
	})
	return err
}

func parseHTTPSURL(name, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s must use http or https", name)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s must include a host", name)
	}
	return u, nil
}
