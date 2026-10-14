package delivery

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// FileCredentials reads the placement token from the file the
// deployment mounts, and fingerprints what the adapter's calls present.
type FileCredentials struct {
	tokenFile string
	presented []Credential
}

// NewFileCredentials reads the placement token from tokenFile, empty
// for a legacy adapter that holds none, and fingerprints presented:
// garam's credential, and control's where the adapter is fenced.
func NewFileCredentials(tokenFile string, presented ...Credential) *FileCredentials {
	return &FileCredentials{tokenFile: tokenFile, presented: presented}
}

// PlacementToken answers the token the file holds, without the
// whitespace a mounted Secret may end in.
func (c *FileCredentials) PlacementToken() (string, error) {
	if c.tokenFile == "" {
		return "", errorx.New(errorx.KindFailedPrecondition, "delivery: no placement token is mounted")
	}
	raw, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return "", errorx.Wrap(err, "delivery: placement token", errorx.KindUnavailable)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", errorx.New(errorx.KindUnavailable, "delivery: the placement token file is empty")
	}
	return token, nil
}

// Fingerprint joins the fingerprint of each credential presented.
func (c *FileCredentials) Fingerprint() (string, error) {
	parts := make([]string, 0, len(c.presented))
	for _, credential := range c.presented {
		part, err := credential.Fingerprint()
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), nil
}

// Certificate is the certificate mode's credential: the agent's
// certificate, which the connection presents per handshake.
type Certificate struct {
	leaf func() (*tls.Certificate, error)
}

// NewCertificate reads the certificate through leaf, which is what the
// handshake presents.
func NewCertificate(leaf func() (*tls.Certificate, error)) *Certificate {
	return &Certificate{leaf: leaf}
}

// Authorization is empty: the connection carries the certificate.
func (c *Certificate) Authorization() (string, error) { return "", nil }

// IsToken is false.
func (c *Certificate) IsToken() bool { return false }

// Fingerprint answers the serial of the certificate the files hold
// now, which renewal changes.
func (c *Certificate) Fingerprint() (string, error) {
	pair, err := c.leaf()
	if err != nil {
		return "", errorx.Wrap(err, "delivery: agent certificate", errorx.KindUnavailable)
	}
	if len(pair.Certificate) == 0 {
		return "", errorx.New(errorx.KindUnavailable, "delivery: no agent certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", errorx.Wrap(err, "delivery: agent certificate", errorx.KindUnavailable)
	}
	return leaf.SerialNumber.String(), nil
}
