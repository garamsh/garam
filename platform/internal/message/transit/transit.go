// Package transit holds the keys garam signs messages with in an
// OpenBao or Vault Transit key, so no private half reaches garam's
// database or memory (ADR-0104). Each garam signing key is one version
// of that key: rotating it makes the next, and signing names the
// version, so a version made by anyone else never signs in its place.
package transit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/keyfile"
	"github.com/garamsh/garam/platform/internal/message"
)

// DefaultMount is the Transit mount when the deployment names none.
const DefaultMount = "transit"

// callTimeout bounds one call to OpenBao. A signature is made inside
// the claim's transaction, so this is also how long a claim holds its
// row, well inside any lease (ADR-0104).
const callTimeout = 5 * time.Second

// maxAnswer bounds what is read of one answer.
const maxAnswer = 1 << 20

// keyType is the one Transit key type garam signs under: ES256's.
const keyType = "ecdsa-p256"

// Settings is the provider's configuration as the deployment states it.
type Settings struct {
	// Address is OpenBao's https base URL.
	Address string
	// CAFile names the PEM root OpenBao's certificate is verified
	// against; the system roots are not consulted.
	CAFile string
	// TokenFile names the owner-only file holding the token garam
	// presents. It is read on every call, so a renewed token takes
	// effect on the next one.
	TokenFile string
	// Mount is the Transit mount, DefaultMount when empty.
	Mount string
	// Key is the name of the ecdsa-p256 key messages are signed under.
	Key string
}

// Custodian is a message.Custodian over one Transit key.
type Custodian struct {
	base      *url.URL
	mount     string
	key       string
	tokenFile string
	client    *http.Client
}

// Compile-time check that Custodian holds message signing keys.
var _ message.Custodian = (*Custodian)(nil)

// New parses settings into a Custodian. It reads the CA file and
// reaches nothing remote, so a start never waits on OpenBao.
func New(s Settings) (*Custodian, error) {
	base, err := url.Parse(s.Address)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil ||
		base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "transit: address must be an https URL")
	}
	mount := s.Mount
	if mount == "" {
		mount = DefaultMount
	}
	if !pathSegments(mount) {
		return nil, errorx.New(errorx.KindInvalidArgument, "transit: mount must be a path with no empty, '.' or '..' segment")
	}
	if s.Key == "" || strings.Contains(s.Key, "/") || s.Key == "." || s.Key == ".." {
		return nil, errorx.New(errorx.KindInvalidArgument, "transit: key must be one path segment")
	}
	if s.TokenFile == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "transit: token file is required")
	}
	root, err := os.ReadFile(s.CAFile)
	if err != nil {
		return nil, errorx.Wrap(err, "transit: CA file", errorx.KindInvalidArgument)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(root) {
		return nil, errorx.New(errorx.KindInvalidArgument, "transit: no certificate in the CA file")
	}
	return &Custodian{
		base:      base,
		mount:     mount,
		key:       s.Key,
		tokenFile: s.TokenFile,
		client: &http.Client{
			Timeout:   callTimeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
			// A redirect would carry the token to wherever it points.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// pathSegments reports whether p is a relative path of ordinary
// segments.
func pathSegments(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// keyInfo is what Transit answers about a key, rotate's answer
// included.
type keyInfo struct {
	Type          string `json:"type"`
	LatestVersion int    `json:"latest_version"`
	Keys          map[string]struct {
		PublicKey string `json:"public_key"`
	} `json:"keys"`
}

// Create rotates the Transit key and answers the version it made, with
// that version's public half taken from the same answer.
func (c *Custodian) Create(ctx context.Context) (*message.HeldKey, error) {
	var info keyInfo
	if err := c.call(ctx, "rotate", "keys/"+c.key+"/rotate", nil, &info); err != nil {
		return nil, err
	}
	if info.Type != keyType {
		return nil, errorx.New(errorx.KindFailedPrecondition,
			"transit: key "+c.key+" is "+info.Type+", not "+keyType)
	}
	version, ok := info.Keys[strconv.Itoa(info.LatestVersion)]
	if info.LatestVersion < 1 || !ok {
		return nil, errorx.New(errorx.KindInternal, "transit: rotate answered no public key for its latest version")
	}
	public, err := uncompressedPoint(version.PublicKey)
	if err != nil {
		return nil, err
	}
	return &message.HeldKey{
		PublicKey: public,
		Held:      message.TransitVersion{Mount: c.mount, Key: c.key, Version: info.LatestVersion},
	}, nil
}

// uncompressedPoint reads a PEM public key as a P-256 point.
func uncompressedPoint(p string) ([]byte, error) {
	block, _ := pem.Decode([]byte(p))
	if block == nil {
		return nil, errorx.New(errorx.KindInternal, "transit: public key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errorx.Wrap(err, "transit: public key", errorx.KindInternal)
	}
	public, ok := parsed.(*ecdsa.PublicKey)
	if !ok || public.Curve != elliptic.P256() {
		return nil, errorx.New(errorx.KindInternal, "transit: public key is not P-256")
	}
	point, err := public.Bytes()
	if err != nil {
		return nil, errorx.Wrap(err, "transit: public key", errorx.KindInternal)
	}
	return point, nil
}

// Holds answers whether the version is of this custodian's key. A
// version of another key or mount is held in a custody this process is
// not configured with (ADR-0104).
func (c *Custodian) Holds(h message.Holding) bool {
	v, ok := h.(message.TransitVersion)
	return ok && v.Mount == c.mount && v.Key == c.key
}

// Open answers a signer bound to the key's version. It calls nothing.
func (c *Custodian) Open(_ context.Context, k *message.SigningKey) (message.KeySigner, error) {
	v, ok := k.Held.(message.TransitVersion)
	if !ok || !c.Holds(v) {
		return nil, errorx.New(errorx.KindInternal, "message: open signing key "+string(k.ID)+": not held by this Transit key")
	}
	return &versionSigner{custodian: c, version: v.Version}, nil
}

// versionSigner signs with one version of the Transit key.
type versionSigner struct {
	custodian *Custodian
	version   int
}

// signRequest asks Transit for an ES256 JWS signature over the input.
type signRequest struct {
	Input               string `json:"input"`
	KeyVersion          int    `json:"key_version"`
	HashAlgorithm       string `json:"hash_algorithm"`
	MarshalingAlgorithm string `json:"marshaling_algorithm"`
}

func (s *versionSigner) Sign(ctx context.Context, input []byte) ([]byte, error) {
	var answer struct {
		Signature string `json:"signature"`
	}
	err := s.custodian.call(ctx, "sign", "sign/"+s.custodian.key, signRequest{
		Input:               base64.StdEncoding.EncodeToString(input),
		KeyVersion:          s.version,
		HashAlgorithm:       "sha2-256",
		MarshalingAlgorithm: "jws",
	}, &answer)
	if err != nil {
		return nil, err
	}
	prefix := "vault:v" + strconv.Itoa(s.version) + ":"
	encoded, ok := strings.CutPrefix(answer.Signature, prefix)
	if !ok {
		return nil, errorx.New(errorx.KindInternal, "transit: signature is not of version "+strconv.Itoa(s.version))
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 64 {
		return nil, errorx.New(errorx.KindInternal, "transit: signature is not ES256's 64 bytes")
	}
	return raw, nil
}

// call sends one request under the mount and decodes the answer's
// data into out. A Transit that cannot be reached, a token not yet
// written, and an answer of 429 or 5xx are unavailable, which a claim
// answers as a server error and a rotation defers; any other refusal
// is garam's misconfiguration. The token travels in its header alone
// and is never part of an error.
func (c *Custodian) call(ctx context.Context, op, path string, body, out any) error {
	token, err := keyfile.Read(c.tokenFile)
	if err != nil {
		return errorx.Wrap(err, "transit: "+op+": token file", errorx.KindUnavailable)
	}
	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errorx.Wrap(err, "transit: "+op, errorx.KindInternal)
		}
		payload = bytes.NewReader(encoded)
	}
	endpoint := c.base.JoinPath("v1", c.mount, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), payload)
	if err != nil {
		return errorx.Wrap(err, "transit: "+op, errorx.KindInternal)
	}
	req.Header.Set("X-Vault-Token", strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return errorx.Wrap(err, "transit: "+op+": unreachable", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return errorx.Wrap(err, "transit: "+op+": answer", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return refusal(op, resp.StatusCode, raw)
	}
	var answer struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || len(answer.Data) == 0 {
		return errorx.New(errorx.KindInternal, "transit: "+op+": answer carries no data")
	}
	if err := json.Unmarshal(answer.Data, out); err != nil {
		return errorx.Wrap(err, "transit: "+op+": answer", errorx.KindInternal)
	}
	return nil
}

// refusal names a status Transit answered and the errors it gave.
func refusal(op string, status int, raw []byte) error {
	var answer struct {
		Errors []string `json:"errors"`
	}
	msg := "transit: " + op + ": HTTP " + strconv.Itoa(status)
	if json.Unmarshal(raw, &answer) == nil && len(answer.Errors) > 0 {
		msg += ": " + strings.Join(answer.Errors, "; ")
	}
	kind := errorx.KindInternal
	if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		kind = errorx.KindUnavailable
	}
	return errorx.New(kind, msg)
}
