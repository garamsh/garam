package delivery

import (
	"os"
	"strings"

	"github.com/garamsh/garam/platform/internal/errorx"
)

// FileCredentials reads the placement token from the file the
// deployment mounts, and fingerprints what the adapter's calls present.
type FileCredentials struct {
	tokenFile string
	presented []Credential
}

// NewFileCredentials reads the placement token from tokenFile and
// fingerprints presented: garam's credential and control's.
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
