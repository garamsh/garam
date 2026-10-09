package machineapi

import (
	"net/http"
	"strings"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
)

// contractValues is one contract's two Garam-Contract-Version values:
// the one a request a certificate authenticated is served under, and
// the one a request a token authenticated is (ADR-0100), each with the
// refusal that names it.
type contractValues struct {
	certificate, token               string
	certificateRefusal, tokenRefusal error
}

var (
	authorityValues = contractValues{operator.ContractVersion, operator.TokenAuthorityContractVersion,
		operator.ErrUnsupportedContract, operator.ErrUnsupportedTokenAuthorityContract}
	executionValues = contractValues{agent.ExecutionContractVersion, agent.TokenExecutionContractVersion,
		agent.ErrUnsupportedExecutionContract, agent.ErrUnsupportedTokenExecutionContract}
	cutoverValues = contractValues{agent.CutoverContractVersion, agent.TokenCutoverContractVersion,
		agent.ErrUnsupportedCutoverContract, agent.ErrUnsupportedTokenCutoverContract}
	enrollmentValues = contractValues{agent.EnrollmentContractVersion, agent.TokenEnrollmentContractVersion,
		agent.ErrUnsupportedEnrollmentContract, agent.ErrUnsupportedTokenEnrollmentContract}
	deliveryValues = contractValues{message.DeliveryContractVersion, message.TokenDeliveryContractVersion,
		message.ErrUnsupportedDeliveryContract, message.ErrUnsupportedTokenDeliveryContract}
)

// served stamps the value the request's credential takes on the answer,
// and refuses a request that asked for another with the refusal naming
// that value, before anything else is read. A request that proved no
// identity takes the certificate's value.
func served(w http.ResponseWriter, r *http.Request, version string, values contractValues) bool {
	expected, refusal := values.certificate, values.certificateRefusal
	if tokenBorne(r) {
		expected, refusal = values.token, values.tokenRefusal
	}
	w.Header().Set(operator.ContractVersionHeader, expected)
	if version != expected {
		writeError(w, refusal)
		return false
	}
	return true
}

// tokenBorne reports whether a token authenticated r, which decides
// the contract value it is served under and where the values that
// share Authorization beside a certificate travel instead (ADR-0100).
func tokenBorne(r *http.Request) bool {
	identity, ok := machine.IdentityOf(r)
	return ok && identity.Method() == machine.MethodToken
}

// operationOf answers the operation authority or reference r carries:
// as `Authorization: Garam-Operation <value>` beside a certificate, and
// in a Garam-Operation header of its own beside a token, which takes
// Authorization (ADR-0100).
func operationOf(r *http.Request) (string, bool) {
	if tokenBorne(r) {
		value := r.Header.Get(operationHeader)
		return value, value != ""
	}
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), operationScheme)
	return value, ok && value != ""
}
