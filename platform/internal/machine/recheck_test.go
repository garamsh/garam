package machine_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
)

// registrations is the Registration seam in memory: every subject is
// held unless withdrawn, and err, where set, is what every read fails
// with.
type registrations struct {
	withdrawn map[grn.GRN]bool
	err       error
}

func (r *registrations) Holds(_ context.Context, subject grn.GRN) error {
	if r.err != nil {
		return r.err
	}
	if r.withdrawn[subject] {
		return errorx.New(errorx.KindNotFound, "no record")
	}
	return nil
}

// identityFor serves one request presenting a token for subject, its
// dates issued and expires, and answers the identity the listener
// handed the handler.
func identityFor(t *testing.T, subject grn.GRN, issued, expires time.Time) machine.Identity {
	t.Helper()
	known := &datedTokens{subject: subject, issued: issued, expires: expires}
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	got := make(chan machine.Identity, 1)
	url := serveTokens(t, dir, known, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machine.IdentityOf(r)
		require.True(t, ok)
		got <- identity
	}))
	_, _, err := get(t, url, roots, "garam_ot_dated")
	require.NoError(t, err)
	return <-got
}

// datedTokens answers every token for subject with the dates given.
type datedTokens struct {
	subject         grn.GRN
	issued, expires time.Time
}

func (d *datedTokens) Resolve(context.Context, string) (machine.Bearer, error) {
	return machine.Bearer{Subject: d.subject, Serial: "t1", IssuedAt: d.issued, ExpiresAt: d.expires}, nil
}

// TestRecheck_HoldsAnIdentityToItsTokenDatesAndItsRecord holds that a
// decision reading an identity again refuses it once its token's dates
// no longer hold or its record is withdrawn, and answers a record that
// cannot be read as unavailable. The identity inside its dates with its
// record held is the control.
func TestRecheck_HoldsAnIdentityToItsTokenDatesAndItsRecord(t *testing.T) {
	ctx := context.Background()
	subject := operatorGRNOf("acme", "one")
	now := time.Now()
	identity := identityFor(t, subject, now.Add(-time.Minute), now.Add(time.Hour))
	records := &registrations{withdrawn: map[grn.GRN]bool{}}
	at := now
	recheck := machine.NewRecheck(records, func() time.Time { return at })

	require.NoError(t, recheck.Recheck(ctx, identity), "the control: dates and record hold")

	at = now.Add(2 * time.Hour)
	assert.ErrorIs(t, recheck.Recheck(ctx, identity), machine.ErrOutsideValidity, "past the token's expiry")
	at = now.Add(-time.Hour)
	assert.ErrorIs(t, recheck.Recheck(ctx, identity), machine.ErrOutsideValidity, "before the token was issued")

	at = now
	records.withdrawn[subject] = true
	assert.True(t, errorx.IsKind(recheck.Recheck(ctx, identity), errorx.KindNotFound), "a withdrawn record")

	records.err = errorx.New(errorx.KindInternal, "the database is gone")
	assert.True(t, errorx.IsKind(recheck.Recheck(ctx, identity), errorx.KindUnavailable), "an unreadable record")
}
