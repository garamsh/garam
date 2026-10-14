// Internal test package: what the intent records is the supervisor's
// own state, and no exported surface reaches an intent whose control
// token is empty or malformed — the drain in recover keeps one from
// being served. The guard on the presented token must hold without
// that, so the test puts the record there itself.
package workload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/internal/grn"
)

// fixedTokens is an agent token source the caller check never reads.
type fixedTokens struct{}

func (fixedTokens) Current(context.Context) (string, error) { return "", nil }

// TestCaller_RefusesAControlTokenThatIsNotOneBeforeComparing holds the
// token mode's caller check to the presented token's spelling: an empty
// token against an empty record, and a malformed one against the same
// malformed record, are each refused with 401 although the
// constant-time comparison alone would accept them. A well-formed
// token matching the record is the control.
func TestCaller_RefusesAControlTokenThatIsNotOneBeforeComparing(t *testing.T) {
	agent := grn.New("acme", grn.DefaultProject, "agent", "0a1b2c3d4e5f6071")
	s := &Supervisor{cfg: Config{Tokens: fixedTokens{}, Desired: Desired{Agent: agent}}}
	wellFormed := controlTokenPrefix + strings.Repeat("a", 64)
	for name, c := range map[string]struct {
		recorded, presented string
		want                int
	}{
		"an empty token against an empty record":        {"", "", http.StatusUnauthorized},
		"a malformed token against the same record":     {"garam_at_xyz", "garam_at_xyz", http.StatusUnauthorized},
		"the control: a well-formed token that matches": {wellFormed, wellFormed, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agent.String()+"/activations", nil)
			r.SetPathValue("agent", agent.String())
			r.Header.Set("Authorization", "Garam-Token "+c.presented)
			w := httptest.NewRecorder()
			_, ok := s.caller(w, r, &intent{ControlToken: c.recorded})
			assert.Equal(t, c.want == http.StatusOK, ok)
			assert.Equal(t, c.want, w.Code)
		})
	}
}
