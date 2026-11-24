// Package cluster owns an organization's record of a connected
// cluster, `grn:<org>/iam/cluster/<id>` (ADR-0184): a user's own
// cluster, reached by nothing garam dials, whose connector
// authenticates as this record.
//
// The record is in one organization, and garam mints its identifier,
// because the connector learns its identity at registration and is
// configured with none before it. It stores no private key, no Secret
// value, no Kubernetes credential and no address of the cluster
// (ADR-0184 decision 8).
package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ServiceType is the GRN service type of a connected cluster.
const ServiceType = "cluster"

// ErrClusterNotFound answers a GRN the organization holds no record
// under.
var ErrClusterNotFound = errorx.New(errorx.KindNotFound, "cluster not found")

// The bounds on what an organization says about its cluster, in
// characters. platform/migrations/000082 holds the same two.
const (
	NameMaxLength        = 100
	DescriptionMaxLength = 1000
)

// identifierBytes is the randomness of a minted identifier, rendered
// as twice as many hex digits.
const identifierBytes = 8

// DetailsPatch is what a caller says about a cluster's name and
// description, each absent where it says nothing. Creating takes it
// for the details the record starts with, and an update for the ones
// it changes; the zero value says nothing, which is valid for both.
// Neither is part of the cluster's identity.
type DetailsPatch struct {
	name        *string
	description *string
}

// NewDetailsPatch parses what a caller said. A name is trimmed and must
// keep at least one character; a description is trimmed and may be
// empty, which clears it. Both are bounded in characters, and both
// must be text PostgreSQL stores: valid UTF-8 without NUL.
func NewDetailsPatch(name, description *string) (DetailsPatch, error) {
	var p DetailsPatch
	if name != nil {
		n := strings.TrimSpace(*name)
		switch {
		case !isStorableText(n):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "a cluster's name must be UTF-8 text without NUL characters")
		case n == "":
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "a cluster's name must not be empty")
		case utf8.RuneCountInString(n) > NameMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "a cluster's name is at most 100 characters")
		}
		p.name = &n
	}
	if description != nil {
		d := strings.TrimSpace(*description)
		switch {
		case !isStorableText(d):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "a cluster's description must be UTF-8 text without NUL characters")
		case utf8.RuneCountInString(d) > DescriptionMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "a cluster's description is at most 1000 characters")
		}
		p.description = &d
	}
	return p, nil
}

func isStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// Name returns the name the patch sets, or nil where it leaves it.
func (p DetailsPatch) Name() *string { return p.name }

// Description returns the description the patch sets, or nil where it
// leaves it.
func (p DetailsPatch) Description() *string { return p.description }

// Cluster is an organization's record of a connected cluster. Every
// field is persisted on the row, and the type holds no invariant over
// them, so it is a data carrier.
type Cluster struct {
	GRN          grn.GRN
	Organization grn.GRN
	// CreatedBy is the member that created the record.
	CreatedBy grn.GRN
	CreatedAt time.Time
	// Name is what the organization calls the cluster. Two clusters may
	// share one; the GRN is what tells them apart.
	Name string
	// Description is what the organization says the cluster is for,
	// empty where it says nothing.
	Description string
}

// New records that orgGRN has a cluster under an identifier garam
// mints. createdBy is the member creating it, and details what it says
// about the cluster; a name it leaves out is the identifier.
func New(orgGRN, createdBy grn.GRN, details DetailsPatch) (*Cluster, error) {
	b := make([]byte, identifierBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, errorx.Wrap(err, "cluster: mint identifier", errorx.KindInternal)
	}
	g, err := GRNOf(orgGRN, hex.EncodeToString(b))
	if err != nil {
		return nil, err
	}
	name := g.Identifier
	if details.name != nil {
		name = *details.name
	}
	var description string
	if details.description != nil {
		description = *details.description
	}
	return &Cluster{
		GRN:          g,
		Organization: orgGRN,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		Name:         name,
		Description:  description,
	}, nil
}

// GRNOf names the cluster of orgGRN under identifier. It is the one
// derivation of that name: creating builds the GRN it stores through
// this, and a route addressing a cluster by its path segment builds the
// same one.
func GRNOf(orgGRN grn.GRN, identifier string) (grn.GRN, error) {
	g := grn.New(orgGRN.Identifier, ServiceType, identifier)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "cluster: identifier", errorx.KindInvalidArgument)
	}
	return g, nil
}
