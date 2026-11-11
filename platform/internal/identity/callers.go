package identity

import (
	"crypto/rsa"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// A service's GRN is the type iam holds (ADR-0142).
const (
	callerType    = "service"
	callerService = "iam"
)

// CallerGRN is the identity the service serving audience authenticates
// as at the introspection endpoint: grn:root/iam/service/<audience>.
func CallerGRN(audience string) grn.GRN {
	return grn.NewIn(grn.RootOwner, callerService, callerType, audience)
}

// Callers authenticates a service by `private_key_jwt` at the
// introspection endpoint, with one key set per audience (ADR-0141 item
// 3), and answers which audience it may introspect. It reuses the
// client assertion of ADR-0102 unchanged: the same parse, the same
// verification, the same lifetime and skew, and each assertion's jti
// spent once, here in this process's memory.
type Callers struct {
	endpoint string
	keys     map[string][]operator.Key
	now      func() time.Time

	mu    sync.Mutex
	spent map[string]time.Time
}

// ParseCallers reads the service keys file: a JSON object from each
// audience to the JSON Web Key Set of the keys its service signs
// assertions with. endpoint is the URL an assertion names as its
// audience, the introspection operation's own. A set naming an audience
// outside [Audiences], or holding a key that is not a public RSA key,
// is refused. A nil now takes time.Now.
func ParseCallers(raw []byte, endpoint string, now func() time.Time) (*Callers, error) {
	var sets map[string]jose.JSONWebKeySet
	if err := json.Unmarshal(raw, &sets); err != nil {
		return nil, errorx.Wrap(err, "identity: service keys are a JSON object of key sets", errorx.KindInvalidArgument)
	}
	keys := make(map[string][]operator.Key, len(sets))
	for audience, set := range sets {
		if !knownAudience(audience) {
			return nil, errorx.New(errorx.KindInvalidArgument, "identity: service keys name an audience the issuer does not serve")
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
			keys[audience] = append(keys[audience], operator.Key{
				Operator: CallerGRN(audience), KID: kid, Algorithm: operator.KeyAlgorithm, Public: public,
			})
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Callers{endpoint: endpoint, keys: keys, now: now, spent: map[string]time.Time{}}, nil
}

// Authenticate returns the audience the service named by clientID
// proved itself as, or [operator.ErrClientInvalid]. A bad signature, a
// service with no key, a wrong identity, audience or time, and a
// replayed assertion are one refusal.
func (c *Callers) Authenticate(clientID, assertion string) (string, error) {
	client, err := grn.Parse(clientID)
	if err != nil || client != CallerGRN(client.Identifier) || !knownAudience(client.Identifier) {
		return "", operator.ErrClientInvalid
	}
	parsed, err := operator.ParseClientAssertion(assertion)
	if err != nil {
		return "", operator.ErrClientInvalid
	}
	for _, key := range c.keys[client.Identifier] {
		if key.KID != parsed.KID() {
			continue
		}
		verified, err := parsed.Verify(key, client, c.endpoint)
		if err != nil {
			return "", operator.ErrClientInvalid
		}
		now := c.now()
		if !verified.Timely(now) || !c.spend(client.Identifier+" "+verified.JTI, verified.ReplayUntil(), now) {
			return "", operator.ErrClientInvalid
		}
		return client.Identifier, nil
	}
	return "", operator.ErrClientInvalid
}

// spend records a jti as used until the time given and reports whether
// it was unused. Entries past their time are dropped as it goes.
func (c *Callers) spend(id string, until, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for spent, expires := range c.spent {
		if !expires.After(now) {
			delete(c.spent, spent)
		}
	}
	if _, used := c.spent[id]; used {
		return false
	}
	c.spent[id] = until
	return true
}
