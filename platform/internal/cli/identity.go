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
func identityIssuer(sessions identity.Sessions, logger *slog.Logger) (*identity.Issuer, error) {
	raw, keyFile := envOr(envIdentityIssuer, ""), envOr(envPolicySigningKeyFile, "")
	if raw == "" {
		return nil, nil
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

// identityCallers reads the service keys file into what authenticates
// a service at introspection. endpoint is the URL an assertion names as
// its audience, derived from the machine token endpoint: one listener
// serves both, and the introspection URL is the token URL with its
// /oauth/token replaced by /oauth/introspect. It returns nil where
// GARAM_IDENTITY_SERVICE_KEYS_FILE is unset, which serves no
// introspection: no service holds a key to ask with.
func identityCallers(tokenURL string) (*identity.Callers, error) {
	file := envOr(envIdentityServiceKeysFile, "")
	if file == "" {
		return nil, nil
	}
	endpoint, ok := strings.CutSuffix(tokenURL, machineTokenPath)
	if !ok {
		return nil, errorx.New(errorx.KindInvalidArgument,
			string(envMachineTokenURL)+" ends in "+machineTokenPath+" where "+string(envIdentityServiceKeysFile)+" is set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, errorx.Wrap(err, string(envIdentityServiceKeysFile), errorx.KindInvalidArgument)
	}
	callers, err := identity.ParseCallers(raw, endpoint+introspectionPathTail, nil)
	if err != nil {
		return nil, errorx.Wrap(err, string(envIdentityServiceKeysFile))
	}
	return callers, nil
}
