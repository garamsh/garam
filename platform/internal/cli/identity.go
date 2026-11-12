package cli

import (
	"log/slog"
	"os"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/signer"
)

// identityIssuer reads GARAM_IDENTITY_ISSUER and the signing key and
// builds garam's OIDC issuer (ADR-0141). It returns nil, and the
// operations that serve the issuer register nothing, when either is
// unset: the issuer is off with no key, and the key is the one the
// policy delivery signs with, so a deployment that sets it for that
// alone has no issuer. An issuer set with no key is logged rather than
// refused for the same reason, since the key's absence is the setting
// that says off.
//
// The policy service trusts one issuer, so a deployment that names both
// this identity and GARAM_POLICY_TOKEN_ISSUER names the same one
// (ADR-0129 item 3): the two set and different is refused here, before
// the key is read.
func identityIssuer(sessions identity.Sessions, logger *slog.Logger) (*identity.Issuer, error) {
	raw, keyFile := envOr(envIdentityIssuer, ""), envOr(envPolicySigningKeyFile, "")
	if raw == "" {
		return nil, nil
	}
	if feed := envOr(envPolicyTokenIssuer, ""); feed != "" && feed != raw {
		return nil, errorx.New(errorx.KindInvalidArgument,
			string(envIdentityIssuer)+" and "+string(envPolicyTokenIssuer)+" are both set and differ: the policy service trusts one issuer")
	}
	if keyFile == "" {
		logger.Warn(string(envIdentityIssuer) + " is set and " + string(envPolicySigningKeyFile) +
			" is not, so the issuer is off")
		return nil, nil
	}
	issuer, err := identity.ParseIssuer(raw)
	if err != nil {
		return nil, errorx.Wrap(err, string(envIdentityIssuer))
	}
	// The identity of the machine listener's tokens is another name,
	// and a token of one must never verify as the other (ADR-0141
	// item 7).
	if issuer == envOr(envMachineIssuer, "") {
		return nil, errorx.New(errorx.KindInvalidArgument,
			string(envIdentityIssuer)+" is not "+string(envMachineIssuer))
	}
	key, err := signer.LoadKey(keyFile)
	if err != nil {
		return nil, errorx.Wrap(err, string(envPolicySigningKeyFile))
	}
	return identity.NewIssuer(issuer, key, sessions, nil)
}

// The machine listener serves introspection beside its token endpoint,
// at the same origin and prefix.
const (
	machineTokenPath      = "/oauth/token"
	introspectionPathTail = "/oauth/introspect"
)

// identityServiceKeys reads the service keys file: each first-party
// service's public keys, by service name. It returns nil where
// GARAM_IDENTITY_SERVICE_KEYS_FILE is unset, and then no service
// principal can authenticate: no service holds a key to ask with. The
// file is read once, at start.
func identityServiceKeys() (identity.ServiceKeys, error) {
	file := envOr(envIdentityServiceKeysFile, "")
	if file == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, errorx.Wrap(err, string(envIdentityServiceKeysFile), errorx.KindInvalidArgument)
	}
	keys, err := identity.ParseServiceKeys(raw)
	if err != nil {
		return nil, errorx.Wrap(err, string(envIdentityServiceKeysFile))
	}
	return keys, nil
}

// introspectionURL derives the URL an assertion names as its audience
// at introspection from the machine token endpoint: one listener serves
// both, and the introspection URL is the token URL with its
// /oauth/token replaced by /oauth/introspect.
func introspectionURL(tokenURL string) (string, error) {
	origin, ok := strings.CutSuffix(tokenURL, machineTokenPath)
	if !ok {
		return "", errorx.New(errorx.KindInvalidArgument,
			string(envMachineTokenURL)+" ends in "+machineTokenPath+" where "+string(envIdentityServiceKeysFile)+" is set")
	}
	return origin + introspectionPathTail, nil
}
