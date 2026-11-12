package identity

import (
	"crypto/rsa"
	"encoding/json"
	"slices"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// A service principal's GRN is of the type service iam holds
// (ADR-0142).
const (
	principalType    = "service"
	principalService = "iam"
)

// ServiceCowork is the first-party service that acts for an
// organization through the policy service (ADR-0129).
const ServiceCowork = "cowork"

// firstParty is the closed list of first-party services, in code and
// not in configuration or a table (ADR-0129 item 1). Every organization
// holds one principal for each of them.
var firstParty = []string{ServiceCowork}

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

// Principal is a service principal an assertion proved.
type Principal struct {
	GRN grn.GRN
	// Service is the first-party service's name, the last segment of
	// GRN.
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

// IsPrincipalClient reports whether clientID names a service
// principal: a GRN of service iam and type service, whichever
// organization and name. It decides which authentication a request
// takes, and says nothing of whether the principal exists.
func IsPrincipalClient(clientID string) bool {
	g, err := grn.Parse(clientID)
	return err == nil && g.Service == principalService && g.ServiceType == principalType
}

// ServiceKeys are the public keys each first-party service signs its
// assertions with, by service name.
type ServiceKeys map[string][]operator.Key

// ParseServiceKeys reads the service keys file: a JSON object from the
// name of each first-party service to the JSON Web Key Set of its
// public keys, at most [MaxServiceKeys] of them. A set naming a
// service off the closed list, holding more keys than that, or holding
// a key that is not a public RSA key is refused. A key's identity is
// its RFC 7638 thumbprint, which is what an assertion's `kid` names.
func ParseServiceKeys(raw []byte) (ServiceKeys, error) {
	var sets map[string]jose.JSONWebKeySet
	if err := json.Unmarshal(raw, &sets); err != nil {
		return nil, errorx.Wrap(err, "identity: service keys are a JSON object of key sets", errorx.KindInvalidArgument)
	}
	keys := make(ServiceKeys, len(sets))
	for service, set := range sets {
		if !IsFirstParty(service) {
			return nil, errorx.New(errorx.KindInvalidArgument, "identity: service keys name a service that is not first-party")
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
