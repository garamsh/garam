package dpop

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
)

// ErrSchemeRefused refuses a bound token presented under any scheme but
// DPoP, Bearer among them (RFC 9449 §7.2): a bound token used as a
// bearer token is one whose holder showed no key.
var ErrSchemeRefused = errorx.New(errorx.KindUnauthenticated, "a bound token is presented as DPoP")

// Proof answers the one proof h carries. None, or more than one, is
// ErrProofInvalid (RFC 9449 §4.3).
func Proof(h http.Header) (string, error) {
	proofs := h.Values(Header)
	if len(proofs) != 1 {
		return "", errorx.Wrap(ErrProofInvalid, "the request carries one DPoP header")
	}
	return proofs[0], nil
}

// Presented answers the token an Authorization header value presents
// under the DPoP scheme, and ErrSchemeRefused for any other, Bearer
// among them, or for none.
func Presented(authorization string) (string, error) {
	scheme, token, ok := strings.Cut(authorization, " ")
	if !ok || !strings.EqualFold(scheme, TokenType) {
		return "", ErrSchemeRefused
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrSchemeRefused
	}
	return token, nil
}

// Bound checks, for a resource server, that r presents token by a
// proof of the key jkt names, its cnf.jkt, for r's method at served,
// the URL garam serves r at; it spends the proof's jti (ADR-0191
// decision 6). It asks for no nonce.
func (v *Verifier) Bound(ctx context.Context, r *http.Request, served, token, jkt string) error {
	if jkt == "" {
		return errorx.New(errorx.KindInternal, "dpop: the token names no key it is bound to")
	}
	proof, err := Proof(r.Header)
	if err != nil {
		return err
	}
	_, err = v.Verify(ctx, proof, Request{Method: r.Method, URL: served, Token: token, Key: jkt})
	return err
}

// Challenge is the WWW-Authenticate value a resource server answers a
// refusal of a bound token with (RFC 9449 §7.1), or empty for an error
// that is not one.
func Challenge(err error) string {
	code := ""
	switch {
	case errors.Is(err, ErrSchemeRefused):
		code = "invalid_token"
	case errors.Is(err, ErrNonceRequired):
		code = "use_dpop_nonce"
	case errors.Is(err, ErrProofInvalid):
		code = "invalid_dpop_proof"
	default:
		return ""
	}
	return TokenType + ` algs="ES256", error="` + code + `"`
}
