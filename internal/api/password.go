package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

const (
	// localPrefix reserves a region of the user identifier space for
	// the accounts garam mints itself. A provider subject may not claim
	// it, so a local account and a provider account can never resolve
	// to one user row — the takeover that a flat subject space would
	// otherwise allow in both directions (ADR-0041).
	localPrefix = "local-"

	// passwordAttemptTTL is how long a recorded attempt holds its slot.
	// It bounds the window a caller who fills the table refuses
	// password logins for, and with it the rate: at most
	// MaxLivePasswordAttempts failed verifications per window, for the
	// whole deployment.
	passwordAttemptTTL = 10 * time.Minute
)

// ErrUsernameInvalid refuses a username that cannot name an account.
// It names the rule and never the value sent.
var ErrUsernameInvalid = errorx.New(errorx.KindInvalidArgument, "username must be lowercase letters, digits or '-'")

// RegisterPassword creates an account from an invitation and signs it
// in. There is no registration without one: the token is the
// unguessable possession proof that authorizes the account, and it is
// also what gates the hash registerClaimed derives — nothing is
// computed for a caller who does not hold a live invitation
// (ADR-0006, ADR-0041).
func (s *Server) RegisterPassword(w http.ResponseWriter, r *http.Request) {
	var body apigen.PasswordRegistration
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	g, err := localUserGRN(body.Username)
	if err != nil {
		writeError(w, err)
		return
	}
	// Refused before the claim, so a password that cannot register
	// never holds the invitation a valid request could have spent.
	if err := authn.CheckNewPassword(authn.Password(body.Password)); err != nil {
		writeError(w, err)
		return
	}
	// The claim spends the invitation before anything is created, so
	// of two registrations on one token the second creates nothing
	// (#1381).
	inv, err := s.orgs.ClaimInvitation(r.Context(), body.InvitationToken)
	if err != nil {
		writeError(w, err)
		return
	}
	u, m, err := s.registerClaimed(r, inv, g, body)
	if err != nil {
		writeError(w, err)
		return
	}
	identify(r, u)
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	nickname := u.Nickname
	writeJSON(w, http.StatusCreated, memberToAPI(m, &nickname))
}

// registerClaimed creates the account a claimed invitation admits:
// the user, its credential and its membership, then names the user on
// the claim. A failure deletes the user, which takes the credential
// and the membership with it, and then releases the claim, so a retry
// with the same username and token finds neither taken.
//
// A process that dies before that cleanup leaves the invitation spent
// with no acceptor named, which fails closed: an admin issues a new
// one. If it dies after the user was created, an account, possibly
// with a password, and no membership remains. It reaches no
// organization, and its username stays taken.
func (s *Server) registerClaimed(r *http.Request, inv *org.Invitation, g grn.GRN, body apigen.PasswordRegistration) (*user.User, *org.Membership, error) {
	ctx := r.Context()
	// Derived only once the invitation is claimed, so the cost is spent
	// for a caller holding a live one and for nobody else.
	verifier, err := authn.NewVerifier(authn.Password(body.Password))
	if err != nil {
		s.releaseClaim(r, inv, g)
		return nil, nil, err
	}
	u := user.New(g, body.Username)
	if err := s.users.Create(ctx, u); err != nil {
		s.releaseClaim(r, inv, g)
		return nil, nil, err
	}
	m := org.NewMembership(inv.Organization, g, inv.Role)
	err = s.credentials.Create(ctx, g, verifier)
	if err == nil {
		err = s.orgs.AddMember(ctx, m)
	}
	if err == nil {
		err = s.orgs.CompleteInvitationClaim(ctx, inv, g)
	}
	if err != nil {
		s.abandonAccount(r, g)
		s.releaseClaim(r, inv, g)
		return nil, nil, err
	}
	return u, m, nil
}

// abandonAccount deletes a user a registration created and could not
// finish. The credential and membership go with it by cascade.
func (s *Server) abandonAccount(r *http.Request, g grn.GRN) {
	ctx := context.WithoutCancel(r.Context())
	if err := s.users.Delete(ctx, g); err != nil {
		slog.WarnContext(ctx, "abandoned registration not deleted",
			slog.String("error", err.Error()),
			slog.String("user_id", g.String()))
	}
}

// releaseClaim returns the invitation a registration claimed to
// pending. It never logs the token, which is the invitation itself.
func (s *Server) releaseClaim(r *http.Request, inv *org.Invitation, g grn.GRN) {
	ctx := context.WithoutCancel(r.Context())
	err := s.orgs.ReleaseInvitationClaim(ctx, inv)
	attrs := []any{
		slog.String("org", inv.Organization.String()),
		slog.String("user_id", g.String()),
	}
	if err != nil {
		slog.WarnContext(ctx, "invitation claim not released",
			append(attrs, slog.String("error", err.Error()))...)
		return
	}
	slog.InfoContext(ctx, "invitation claim released", attrs...)
}

// LoginPassword signs in with a username and a password.
func (s *Server) LoginPassword(w http.ResponseWriter, r *http.Request) {
	var body apigen.PasswordLogin
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	u, err := s.verifyPassword(r, body.Username, authn.Password(body.Password))
	if err != nil {
		writeError(w, err)
		return
	}
	identify(r, u)
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.User{Grn: u.GRN.String(), Nickname: u.Nickname})
}

// verifyPassword resolves the username to the user its password
// authenticates, or refuses. A wrong password and a username no
// credential belongs to leave by the same return with the same error:
// they are one answer, because telling them apart publishes the
// account list (ADR-0041).
func (s *Server) verifyPassword(r *http.Request, username string, p authn.Password) (*user.User, error) {
	g, err := localUserGRN(username)
	if err != nil {
		// A username that cannot name an account is refused as one
		// that names none. What this does reveal is whether a string
		// is a well-formed username, which the API specification
		// already states.
		return nil, authn.ErrPasswordRejected
	}
	attempt, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	// Recorded before anything is hashed, so the bound is what decides
	// whether the computation happens at all (ADR-0041).
	if err := s.attempts.Record(r.Context(), attempt, passwordAttemptTTL); err != nil {
		return nil, err
	}
	verifier, err := s.credentials.Get(r.Context(), g)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			// Spends the computation a stored credential would have
			// spent, so the two refusals cost alike as well as read
			// alike.
			return nil, authn.RejectAbsent(p)
		}
		return nil, err
	}
	if err := verifier.Match(p); err != nil {
		return nil, err
	}
	u, err := s.users.Get(r.Context(), g)
	if err != nil {
		return nil, err
	}
	s.upgradeVerifier(r, g, verifier, p)
	// The slot is freed only by a verification that succeeded, so what
	// accumulates against the bound is failures.
	if err := s.attempts.Release(r.Context(), attempt); err != nil {
		slog.WarnContext(r.Context(), "password attempt not released",
			slog.String("error", err.Error()),
			slog.String("user_id", g.String()))
	}
	return u, nil
}

// upgradeVerifier re-hashes a verified password when what is stored
// was computed under parameters other than the current policy, which
// is what converges the stored population onto one setting without
// asking anyone to reset (ADR-0041). The sign-in has already
// succeeded, so a failure here is reported and not raised: the upgrade
// is maintenance, and refusing a valid sign-in over it would be worse
// than carrying the old parameters one login longer.
func (s *Server) upgradeVerifier(r *http.Request, g grn.GRN, held authn.Verifier, p authn.Password) {
	if held.AtCurrentPolicy() {
		return
	}
	fresh, err := authn.NewVerifier(p)
	if err != nil {
		slog.WarnContext(r.Context(), "password verifier not upgraded",
			slog.String("error", err.Error()),
			slog.String("user_id", g.String()))
		return
	}
	if err := s.credentials.Replace(r.Context(), g, fresh); err != nil {
		slog.WarnContext(r.Context(), "password verifier not upgraded",
			slog.String("error", err.Error()),
			slog.String("user_id", g.String()))
	}
}

// startSession issues a session for u and sets the session cookie. It
// is what a password sign-in shares with the OAuth callback: a
// password authenticates and does not become a session mechanism of
// its own (ADR-0005).
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *user.User) error {
	token, err := authn.NewToken()
	if err != nil {
		return err
	}
	if err := s.sessions.Create(r.Context(), token, authn.NewSession(u.GRN, s.cfg.SessionTTL)); err != nil {
		return err
	}
	s.setCookie(w, sessionCookie, token, s.cfg.SessionTTL)
	return nil
}

// localUserGRN builds the GRN for the local account a username names.
// The reserved prefix is what keeps that account and any provider's
// from ever being the same row.
func localUserGRN(username string) (grn.GRN, error) {
	if username == "" {
		return grn.GRN{}, ErrUsernameInvalid
	}
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, localPrefix+username)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, ErrUsernameInvalid
	}
	return g, nil
}

// reservedSubject reports whether a provider asserted a subject in the
// space garam mints local accounts from. Such a subject is refused
// rather than adopted: honouring it would hand the provider's caller a
// local account's row (ADR-0041).
func reservedSubject(subject string) bool {
	return strings.HasPrefix(subject, localPrefix)
}
