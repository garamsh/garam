package page

import (
	"context"
	"crypto/rand"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// idAlphabet and idLength make an ID a GRN identifier accepts: 12
// characters of 32, 60 bits, drawn from crypto/rand.
const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"
	idLength   = 12
)

func newID() string {
	b := make([]byte, idLength)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b)
}

// Create adds a page the caller writes, under a parent it writes.
func (s *service) Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Page, error) {
	if len(in.Title) > maxTitle {
		return Page{}, ErrTitleTooLong
	}
	checks := []platform.Check{{Action: platform.ActionPageWrite}}
	if in.Parent != "" {
		checks = append(checks, platform.OnPage(platform.ActionPageWrite, in.Parent))
	}
	if err := platform.Require(ctx, s.platform, caller, org, checks...); err != nil {
		return Page{}, err
	}
	return s.repo.Create(ctx, Page{
		ID:        newID(),
		Org:       org,
		Parent:    in.Parent,
		Title:     in.Title,
		CreatedBy: caller.GRN,
	})
}
