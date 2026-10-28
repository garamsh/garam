package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
)

// TokenScheme is the Authorization scheme an agent token travels under
// (ADR-0100).
const TokenScheme = "Garam-Token"

// agentToken is an agent token as api/machine.yaml spells one.
var agentToken = regexp.MustCompile(`^garam_at_[0-9a-f]{64}$`)

// maxTokenFile bounds what is read of a token file: a token and the
// whitespace a mounted Secret may end in.
const maxTokenFile = 1 << 10

// TokenFile is the adapter's credential: an agent token its
// assignee writes to a file and replaces before it expires. It is read
// before every call and never held, so a replacement is presented on
// the next call without a restart (ADR-0100).
type TokenFile struct {
	path string
}

// NewTokenFile reads the token from path.
func NewTokenFile(path string) (*TokenFile, error) {
	if path == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: a token file is required")
	}
	return &TokenFile{path: path}, nil
}

// Read answers the token the file holds now, less the carriage
// returns and newlines that end it. A file that is missing, or holds
// anything else but one whole token — leading or inner whitespace
// included — is KindUnavailable: a writer that does not rename may be
// part way through, and the next read is the retry.
func (f *TokenFile) Read() (string, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return "", errorx.Wrap(err, "delivery: token file", errorx.KindUnavailable)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxTokenFile+1))
	if err != nil {
		return "", errorx.Wrap(err, "delivery: token file", errorx.KindUnavailable)
	}
	token := strings.TrimRight(string(raw), "\r\n")
	if len(raw) > maxTokenFile || !agentToken.MatchString(token) {
		return "", errorx.New(errorx.KindUnavailable, "delivery: "+f.path+" does not hold one whole agent token")
	}
	return token, nil
}

// Authorization answers the token under its scheme.
func (f *TokenFile) Authorization() (string, error) {
	token, err := f.Read()
	if err != nil {
		return "", err
	}
	return TokenScheme + " " + token, nil
}

// Fingerprint answers the token's SHA-256, so the token itself never
// reaches what keeps the fingerprint.
func (f *TokenFile) Fingerprint() (string, error) {
	token, err := f.Read()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]), nil
}
