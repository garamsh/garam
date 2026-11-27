package bot

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
)

// The bounds on a declarations poll (DECL-3, ADR-0187 decision 6).
const (
	// MaxWait is the longest a poll is held, and the default.
	MaxWait = 50 * time.Second
	// DeclarationRecheck is how often a held poll reads its record
	// again whatever it was told, so a wake that was lost costs at most
	// this long.
	DeclarationRecheck = 5 * time.Second
)

// WatchDeclarations reads caller's page after after, and holds an empty
// one until a write wakes it, its recheck reads a change, or wait is
// out.
func (s *service) WatchDeclarations(ctx context.Context, caller grn.GRN, after *string, wait time.Duration) (Page, error) {
	if !app.IsInstanceGRN(caller) {
		return Page{}, operator.ErrClientInvalid
	}
	if after != nil && *after == "" {
		return Page{}, errorx.New(errorx.KindInvalidArgument, "after is empty")
	}
	if wait < 0 || wait > MaxWait {
		return Page{}, errorx.New(errorx.KindInvalidArgument, "wait is not from 0 to 50 seconds")
	}
	if err := s.watches.Check(caller.String()); err != nil {
		return Page{}, err
	}
	// Subscribed before the first read, so a write between the read and
	// the wait still wakes it.
	woken, stop := s.cfg.Wakes.Subscribe(caller)
	defer stop()
	held := time.NewTimer(wait)
	defer held.Stop()
	recheck := time.NewTicker(s.cfg.DeclarationRecheck)
	defer recheck.Stop()
	expired := false
	for {
		page, err := s.declarations.DeclarationPage(ctx, caller, after, s.cfg.DeclarationPageSize)
		if errors.Is(err, app.ErrInstanceNotFound) {
			return Page{}, operator.ErrClientInvalid
		}
		if err != nil {
			return Page{}, err
		}
		if page.Full || len(page.Changes) > 0 || expired {
			return page, nil
		}
		select {
		case <-woken:
		case <-recheck.C:
		case <-held.C:
			expired = true
		case <-ctx.Done():
			return Page{}, errorx.Wrap(ctx.Err(), "watch declarations", errorx.KindCanceled)
		}
	}
}
