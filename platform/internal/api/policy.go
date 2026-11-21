package api

import (
	"github.com/garamsh/garam/lib/grn"
)

// serviceTypePolicy is the GRN service type of a stored policy. The
// policy service owns policies (ADR-0146), so the api server addresses
// them by name rather than importing it.
const serviceTypePolicy = "policy"

// policyGRN builds the GRN of a policy owned by the org. The org
// segment scopes the identifier, so a policy of another org is not
// addressable through that org's paths.
func policyGRN(orgSlug, policySlug string) grn.GRN {
	return grn.New(orgSlug, serviceTypePolicy, policySlug)
}
