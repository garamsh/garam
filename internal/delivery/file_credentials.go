package delivery

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// FileCredentials reads the placement token and the agent certificate
// from the files the deployment mounts.
type FileCredentials struct {
	tokenFile string
	leaf      func() (*tls.Certificate, error)
}

// NewFileCredentials reads the placement token from tokenFile, empty for
// a legacy adapter that holds none, and the certificate through leaf,
// which is what the handshake presents.
func NewFileCredentials(tokenFile string, leaf func() (*tls.Certificate, error)) *FileCredentials {
	return &FileCredentials{tokenFile: tokenFile, leaf: leaf}
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

// LeafSerial answers the serial of the certificate the files hold now.
func (c *FileCredentials) LeafSerial() (string, error) {
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
