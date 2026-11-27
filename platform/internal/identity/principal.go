package identity

import (
	"crypto/rsa"
	"encoding/json"
	"os"
	"slices"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
)

// A service principal's GRN is of the type service iam holds
// (ADR-0142).
const (
	principalType    = "service"
	principalService = "iam"
)

// The services garam issues a principal for.
const (
	// ServiceCowork acts for an organization through the policy
	// service (ADR-0129).
	ServiceCowork = "cowork"
	// ServiceCodehub is a service garam does not deploy, which acts for
	// an organization through the policy service as cowork does
	// (ADR-0143 item 7).
	ServiceCodehub = "codehub"
)

// firstParty is the closed list of first-party services, in code and
// not in configuration or a table (ADR-0129 item 1, ADR-0141 item 13).
// First-party means a service garam issues a principal for, whether or
// not garam deploys it. Every organization holds one principal for
// each of them.
var firstParty = []string{ServiceCowork, ServiceCodehub}

// FirstParty returns the closed list of first-party services a
// principal is written for with each organization.
func FirstParty() []string { return slices.Clone(firstParty) }

// IsFirstParty reports whether name is on the closed list.
func IsFirstParty(name string) bool { return slices.Contains(firstParty, name) }

// MaxServiceKeys is how many public keys a service may hold at once:
// the one it signs with and its successor while one is rotated in.
const MaxServiceKeys = 2

// PrincipalGRN is the identity service holds in the organization
// orgID: grn:<orgID>/iam/service/<service>.
func PrincipalGRN(orgID, service string) grn.GRN {
	return grn.NewIn(orgID, principalService, principalType, service)
}

// Principal is a service principal or an installation an assertion
// proved.
type Principal struct {
	GRN grn.GRN
	// Service is the name its keys are held under, the last segment of
	// GRN: a first-party service's name, or an installation's app's
	// slug.
	Service string
}

// parsePrincipal reads a client_id as a service principal's GRN of a
// first-party service, and reports false for any other.
func parsePrincipal(clientID string, services func(string) bool) (Principal, bool) {
	g, err := grn.Parse(clientID)
	if err != nil || g.Organization == "" || g.Organization == grn.RootOwner ||
		g != PrincipalGRN(g.Organization, g.Identifier) || !services(g.Identifier) {
		return Principal{}, false
	}
	return Principal{GRN: g, Service: g.Identifier}, true
}

// parseInstallation reads a client_id as the GRN of an installation in
// an organization, `grn:<org>/iam/installation/<slug>`, of an app whose
// slug holds keys, and reports false for any other (ADR-0192 decision
// 5). The installation's identifier is its app's slug, so the keys are
// found with no lookup.
func parseInstallation(clientID string, slugs func(string) bool) (Principal, bool) {
	g, err := grn.Parse(clientID)
	if err != nil || g.Organization == "" || g.Organization == grn.RootOwner ||
		g != grn.NewIn(g.Organization, principalService, app.InstallationType, g.Identifier) || !slugs(g.Identifier) {
		return Principal{}, false
	}
	return Principal{GRN: g, Service: g.Identifier}, true
}

// IsInstallationClient reports whether clientID names an installation:
// a GRN of service iam and type installation. As [IsPrincipalClient]
// does, it decides which authentication a request takes and says
// nothing of whether the installation exists.
func IsInstallationClient(clientID string) bool {
	g, err := grn.Parse(clientID)
	return err == nil && g.Service == principalService && g.ServiceType == app.InstallationType
}

// IsPrincipalClient reports whether clientID names a service
// principal: a GRN of service iam and type service, whichever
// organization and name. It decides which authentication a request
// takes, and says nothing of whether the principal exists.
func IsPrincipalClient(clientID string) bool {
	g, err := grn.Parse(clientID)
	return err == nil && g.Service == principalService && g.ServiceType == principalType
}

// ServiceKeys are the public keys each first-party service and each
// platform app signs its assertions with, by the service's name or the
// app's slug (ADR-0192 decision 5).
type ServiceKeys map[string][]operator.Key

// ParseServiceKeys reads the service keys file: a JSON object from the
// name of each first-party service or the slug of each platform app to
// the JSON Web Key Set of its public keys, at most [MaxServiceKeys] of
// them. A first-party name is a slug, so cowork's and codehub's sets
// are each read both ways until #2085 makes them apps. A set named by
// what is not a slug, holding more keys than that, or holding a key
// that is not a public RSA key is refused. A key's identity is
// its RFC 7638 thumbprint, which is what an assertion's `kid` names.
func ParseServiceKeys(raw []byte) (ServiceKeys, error) {
	var sets map[string]jose.JSONWebKeySet
	if err := json.Unmarshal(raw, &sets); err != nil {
		return nil, errorx.Wrap(err, "identity: service keys are a JSON object of key sets", errorx.KindInvalidArgument)
	}
	keys := make(ServiceKeys, len(sets))
	for service, set := range sets {
		if _, err := app.AppGRN(grn.RootOwner, service); err != nil {
			return nil, errorx.New(errorx.KindInvalidArgument, "identity: service keys name a set that is not a slug")
		}
		if len(set.Keys) > MaxServiceKeys {
			return nil, errorx.New(errorx.KindInvalidArgument, "identity: a service holds at most two keys")
		}
		for _, k := range set.Keys {
			public, ok := k.Key.(*rsa.PublicKey)
			if !ok || !k.IsPublic() {
				return nil, errorx.New(errorx.KindInvalidArgument, "identity: a service key is not a public RSA key")
			}
			kid, err := operator.Thumbprint(public)
			if err != nil {
				return nil, err
			}
			keys[service] = append(keys[service], operator.Key{KID: kid, Algorithm: operator.KeyAlgorithm, Public: public})
		}
	}
	return keys, nil
}

// KeySource answers each first-party service's and platform app's
// public keys. It is
// asked on every authentication (ADR-0129 item 2), so removing a
// compromised key is an edit to the source and not a restart. An error
// is the source's own failure, of the kind unavailable.
type KeySource interface {
	ServiceKeys() (ServiceKeys, error)
}

// FixedKeys is a [KeySource] that always answers keys.
type FixedKeys ServiceKeys

// ServiceKeys answers the keys the source holds.
func (f FixedKeys) ServiceKeys() (ServiceKeys, error) { return ServiceKeys(f), nil }

// KeysFile is a [KeySource] that reads and parses the file at the path
// on every call. The file holds at most two keys a service, so the
// read is small. A file that is gone or does not parse is unavailable.
type KeysFile string

// ServiceKeys reads and parses the file.
func (f KeysFile) ServiceKeys() (ServiceKeys, error) {
	raw, err := os.ReadFile(string(f))
	if err != nil {
		return nil, errorx.Wrap(err, "identity: service keys file", errorx.KindUnavailable)
	}
	keys, err := ParseServiceKeys(raw)
	if err != nil {
		return nil, errorx.Wrap(err, "identity: service keys file", errorx.KindUnavailable)
	}
	return keys, nil
}
