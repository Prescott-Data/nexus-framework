package samlutil

import (
	"net/url"
	"strings"

	"github.com/crewjam/saml"
)

// BuildServiceProvider constructs a SAML ServiceProvider instance
// using the metadata from the provider profile.
func BuildServiceProvider(spEntityID, acsURL, idpEntityID, idpSSOURL, idpCert string) (*saml.ServiceProvider, error) {

	spURL, err := url.Parse(acsURL)
	if err != nil {
		return nil, err
	}

	idpMetadata := &saml.EntityDescriptor{
		EntityID: idpEntityID,
		IDPSSODescriptors: []saml.IDPSSODescriptor{
			{
				SingleSignOnServices: []saml.Endpoint{
					{
						Binding:  saml.HTTPRedirectBinding,
						Location: idpSSOURL,
					},
					{
						Binding:  saml.HTTPPostBinding,
						Location: idpSSOURL,
					},
				},
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						KeyDescriptors: []saml.KeyDescriptor{
							{
								Use: "signing",
								KeyInfo: saml.KeyInfo{
									X509Data: saml.X509Data{
										X509Certificates: []saml.X509Certificate{
											{Data: formatCert(idpCert)},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return &saml.ServiceProvider{
		EntityID:          spEntityID,
		MetadataURL:       *spURL,
		AcsURL:            *spURL,
		IDPMetadata:       idpMetadata,
		AllowIDPInitiated: true,
		ValidateRequestID: func(response saml.Response, possibleRequestIDs []string) error {
			return nil
		},
	}, nil
}

// formatCert strips PEM headers and whitespace to get raw base64.
func formatCert(cert string) string {
	cert = strings.TrimSpace(cert)

	lines := strings.Split(cert, "\n")
	var out string
	for _, line := range lines {
		if !strings.Contains(line, "BEGIN") && !strings.Contains(line, "END") {
			out += strings.TrimSpace(line)
		}
	}
	return out
}
