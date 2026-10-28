package machineapi

import (
	"net/http"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
)

// contractValue is the Garam-Contract-Version value one contract is
// served under, with the refusal that names it. Every caller presents
// a token, so each contract has one value (ADR-0100, ADR-0118).
type contractValue struct {
	version string
	refusal error
}

var (
	authorityValues  = contractValue{operator.TokenAuthorityContractVersion, operator.ErrUnsupportedTokenAuthorityContract}
	executionValues  = contractValue{agent.TokenExecutionContractVersion, agent.ErrUnsupportedTokenExecutionContract}
	enrollmentValues = contractValue{agent.TokenEnrollmentContractVersion, agent.ErrUnsupportedTokenEnrollmentContract}
	deliveryValues   = contractValue{message.TokenDeliveryContractVersion, message.ErrUnsupportedTokenDeliveryContract}
)

// served stamps the contract's value on the answer, and refuses a
// request that asked for another with the refusal naming that value,
// before anything else is read.
func served(w http.ResponseWriter, version string, value contractValue) bool {
	w.Header().Set(operator.ContractVersionHeader, value.version)
	if version != value.version {
		writeError(w, value.refusal)
		return false
	}
	return true
}

// operationOf answers the operation authority or reference r carries,
// in a Garam-Operation header of its own, since Authorization carries
// the caller's token (ADR-0100).
func operationOf(r *http.Request) (string, bool) {
	value := r.Header.Get(operationHeader)
	return value, value != ""
}
