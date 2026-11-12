// Package grpc sends the feed's entries to the policy service's
// ApplyEntityChanges, as the organization they belong to.
package grpc

import (
	"context"
	"fmt"

	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// Applier is the policy service as the sender calls it: one call, made
// under the token of the organization it names.
type Applier interface {
	ApplyEntityChanges(
		ctx context.Context, org string, req *policyv1.ApplyEntityChangesRequest,
	) (*policyv1.ApplyEntityChangesResponse, error)
}

// Sender implements policyfeed.Sender.
type Sender struct {
	applier Applier
}

var _ policyfeed.Sender = (*Sender)(nil)

// NewSender constructs a sender over applier.
func NewSender(applier Applier) *Sender {
	return &Sender{applier: applier}
}

const (
	service = "cowork"
	orgType = "Iam::Org"
)

// Send applies entries of org at sequence. An upsert names the
// organization's Iam::Org as the entity's org and as its parent, which
// is all cowork's tables know of where an entity sits. An entry whose
// identifiers no GRN can hold is never sent: a live one is refused
// here, as the service would refuse it, and a deleted one has nothing
// to delete.
func (s *Sender) Send(
	ctx context.Context, org string, sequence int64, entries []policyfeed.Entry,
) ([]policyfeed.Refusal, error) {
	var (
		changes []*policyv1.EntityChange
		sent    []policyfeed.Entry
		refused []policyfeed.Refusal
	)
	orgRef := &policyv1.EntityRef{Type: orgType, Id: grn.Org(org).String()}
	for _, e := range entries {
		ref, err := entityRef(e.Entity)
		if err != nil {
			if e.Exists {
				refused = append(refused, policyfeed.Refusal{Entity: e.Entity, Reason: err.Error()})
			}
			continue
		}
		change := &policyv1.EntityChange{Entity: ref, Sequence: sequence}
		if e.Exists {
			change.Operation = &policyv1.EntityChange_Upsert{Upsert: &policyv1.EntityUpsert{
				Org:     orgRef,
				Parents: []*policyv1.EntityRef{orgRef},
			}}
		} else {
			change.Operation = &policyv1.EntityChange_Delete{Delete: &policyv1.EntityDelete{}}
		}
		changes = append(changes, change)
		sent = append(sent, e)
	}
	if len(changes) == 0 {
		return refused, nil
	}
	resp, err := s.applier.ApplyEntityChanges(ctx, org, &policyv1.ApplyEntityChangesRequest{Changes: changes})
	if err != nil {
		return nil, err
	}
	for _, r := range resp.GetRefusals() {
		if int(r.GetIndex()) >= len(sent) {
			return nil, errorx.New(errorx.KindInternal, fmt.Sprintf(
				"policy service refused change %d of a request of %d", r.GetIndex(), len(sent)))
		}
		refused = append(refused, policyfeed.Refusal{Entity: sent[r.GetIndex()].Entity, Reason: r.GetError()})
	}
	return refused, nil
}

// entityRef names an entity by its qualified type and GRN, both read
// from lib/grn.
func entityRef(e policyfeed.Entity) (*policyv1.EntityRef, error) {
	info, ok := grn.Types(service)[e.Type]
	if !ok {
		return nil, errorx.New(errorx.KindInvalidArgument, "cowork has no entity type "+e.Type)
	}
	name := grn.NewIn(e.Org, service, e.Type, e.ID)
	if err := name.Validate(); err != nil {
		return nil, err
	}
	return &policyv1.EntityRef{Type: info.Entity, Id: name.String()}, nil
}
