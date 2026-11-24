// Package app owns the systems that connect to garam (ADR-0192): an
// app, registered once; its installation in an organization; and the
// instances of that installation, each a running copy of the app that
// registers with a one-time token and authenticates with a key it
// generated.
//
// Every difference between apps is a setting on the app's record, and
// no path here branches on which app it is or on an instance's kind.
// An instance stores no private key and no address of the system it
// runs on (ADR-0184 decision 8, read for every instance).
package app

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// The GRN types of the app system, all under the iam service.
const (
	AppType          = "app"
	InstallationType = "installation"
	InstanceType     = "instance"
)

// The sentinels of the records.
var (
	// ErrAppNotFound answers an app that is not there, or one the
	// organization may not see: another organization's private app.
	ErrAppNotFound = errorx.New(errorx.KindNotFound, "app not found")
	// ErrInstallationNotFound answers a GRN the organization holds no
	// installation under.
	ErrInstallationNotFound = errorx.New(errorx.KindNotFound, "installation not found")
	// ErrInstallationExists refuses a second installation of one app in
	// one organization.
	ErrInstallationExists = errorx.New(errorx.KindAlreadyExists, "the app is already installed in the organization")
	// ErrInstanceNotFound answers a GRN the installation holds no
	// instance under.
	ErrInstanceNotFound = errorx.New(errorx.KindNotFound, "instance not found")
	// ErrNoInstances refuses an instance of an app whose instance
	// setting is none.
	ErrNoInstances = errorx.New(errorx.KindNotFound, "the app runs no instances")
	// ErrInstanceLimit refuses a second instance of an app whose
	// instance setting is one.
	ErrInstanceLimit = errorx.New(errorx.KindAlreadyExists, "the installation already holds its one instance")
)

// Visibility is who may install an app: its own organization alone, or
// every organization.
type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityPublic  Visibility = "public"
)

// Instances is how many instances one installation of an app holds.
type Instances string

const (
	InstancesNone Instances = "none"
	InstancesOne  Instances = "one"
	InstancesMany Instances = "many"
)

// Admit answers whether an installation holding held instances may
// hold one more under the setting.
func (n Instances) Admit(held int) error {
	switch n {
	case InstancesMany:
		return nil
	case InstancesOne:
		if held == 0 {
			return nil
		}
		return ErrInstanceLimit
	case InstancesNone:
		return ErrNoInstances
	}
	return errorx.New(errorx.KindInternal, "app: unknown instance setting "+string(n))
}

// Delivery is how events reach an app. Only pull is built: webhook
// delivery arrives with its URL (#2083).
type Delivery string

const DeliveryPull Delivery = "pull"

// App is an app's record. Every field is a setting a path reads, and
// the type holds no invariant over them, so it is a data carrier. Its
// owner is its GRN's organization: `root` for the platform's own.
type App struct {
	GRN         grn.GRN
	Slug        string
	Name        string
	Description string
	Visibility  Visibility
	// RequestedActions are the single action ids an installation of it
	// is granted, each as a permit on the installation.
	RequestedActions []string
	// RequestsBotHosting is whether it asks to host bots, which an
	// installation's owner grants apart from the requested actions.
	RequestsBotHosting bool
	Instances          Instances
	Delivery           Delivery
	// MachineOperations are the machine listener's routes, as its
	// router matches them, that its instances' tokens reach beyond the
	// app system's own.
	MachineOperations []string
}

// InstallableIn reports whether an organization may install the app:
// any may install a public app, and only its owner a private one.
func (a *App) InstallableIn(orgGRN grn.GRN) bool {
	return a.Visibility == VisibilityPublic || a.GRN.Organization == orgGRN.Identifier
}

// AppGRN names the app registered under slug by owner, the
// organization's identifier or grn.RootOwner.
func AppGRN(owner, slug string) (grn.GRN, error) {
	g := grn.New(owner, AppType, slug)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "app: slug", errorx.KindInvalidArgument)
	}
	return g, nil
}

// Scope is what an installation's grants reach: the whole organization,
// or the projects it names. The zero value is the whole organization.
type Scope struct {
	projects []grn.GRN
}

// ProjectScope scopes an installation to projects, each a project of
// orgGRN. An empty list is refused: the whole organization is the zero
// Scope, and no scope reaches nothing.
func ProjectScope(orgGRN grn.GRN, projects []grn.GRN) (Scope, error) {
	if len(projects) == 0 {
		return Scope{}, errorx.New(errorx.KindInvalidArgument, "a project scope names at least one project")
	}
	seen := make([]grn.GRN, 0, len(projects))
	for _, p := range projects {
		if p != grn.New(orgGRN.Identifier, "project", p.Identifier) {
			return Scope{}, errorx.New(errorx.KindInvalidArgument, p.String()+" is not a project of "+orgGRN.String())
		}
		if !slices.Contains(seen, p) {
			seen = append(seen, p)
		}
	}
	return Scope{projects: seen}, nil
}

// Projects returns the projects the scope names, empty for the whole
// organization.
func (s Scope) Projects() []grn.GRN { return slices.Clone(s.projects) }

// IsOrganization reports whether the scope is the whole organization.
func (s Scope) IsOrganization() bool { return len(s.projects) == 0 }

// Installation is one app installed in one organization.
type Installation struct {
	GRN          grn.GRN
	Organization grn.GRN
	App          grn.GRN
	// Incarnation tells this installation from an earlier one of the
	// same app, which held the same GRN.
	Incarnation string
	Scope       Scope
	CreatedBy   grn.GRN
	CreatedAt   time.Time
}

// NewInstallation installs a in orgGRN over scope, for createdBy. An app
// the organization may not install is ErrAppNotFound, as one it may not
// see is.
func NewInstallation(orgGRN grn.GRN, a *App, createdBy grn.GRN, scope Scope) (*Installation, error) {
	if !a.InstallableIn(orgGRN) {
		return nil, ErrAppNotFound
	}
	g, err := InstallationGRN(orgGRN, a.Slug)
	if err != nil {
		return nil, err
	}
	return &Installation{GRN: g, Organization: orgGRN, App: a.GRN, Scope: scope,
		CreatedBy: createdBy, CreatedAt: time.Now().UTC()}, nil
}

// InstallationGRN names the installation in orgGRN of the app under
// slug. A service derives it from the organization with no lookup.
func InstallationGRN(orgGRN grn.GRN, slug string) (grn.GRN, error) {
	g := grn.New(orgGRN.Identifier, InstallationType, slug)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "installation: slug", errorx.KindInvalidArgument)
	}
	return g, nil
}

// The bounds on what an organization says about an instance, in
// characters. platform/migrations/000082 and 000088 hold the same.
const (
	NameMaxLength        = 100
	DescriptionMaxLength = 1000
	KindMaxLength        = 64
)

// identifierBytes is the randomness of a minted identifier, rendered
// as twice as many hex digits.
const identifierBytes = 8

// DetailsPatch is what a caller says about an instance's name,
// description and kind, each absent where it says nothing. Creating
// takes it for the details the record starts with, and an update for
// the ones it changes; the zero value says nothing, which is valid for
// both. None is part of the instance's identity, and garam never reads
// the kind.
type DetailsPatch struct {
	name        *string
	description *string
	kind        *string
}

// NewDetailsPatch parses what a caller said. A name is trimmed and must
// keep at least one character; a description and a kind are trimmed
// and may be empty, which clears them. Each is bounded in characters,
// and each must be text PostgreSQL stores: valid UTF-8 without NUL.
func NewDetailsPatch(name, description, kind *string) (DetailsPatch, error) {
	var p DetailsPatch
	if name != nil {
		n := strings.TrimSpace(*name)
		switch {
		case !isStorableText(n):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's name must be UTF-8 text without NUL characters")
		case n == "":
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's name must not be empty")
		case utf8.RuneCountInString(n) > NameMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's name is at most 100 characters")
		}
		p.name = &n
	}
	if description != nil {
		d := strings.TrimSpace(*description)
		switch {
		case !isStorableText(d):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's description must be UTF-8 text without NUL characters")
		case utf8.RuneCountInString(d) > DescriptionMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's description is at most 1000 characters")
		}
		p.description = &d
	}
	if kind != nil {
		k := strings.TrimSpace(*kind)
		switch {
		case !isStorableText(k):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's kind must be UTF-8 text without NUL characters")
		case utf8.RuneCountInString(k) > KindMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an instance's kind is at most 64 characters")
		}
		p.kind = &k
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

// Kind returns the kind the patch sets, or nil where it leaves it.
func (p DetailsPatch) Kind() *string { return p.kind }

// Instance is one running copy of an installation's app. Every field is
// persisted on the row, and the type holds no invariant over them, so
// it is a data carrier.
type Instance struct {
	GRN          grn.GRN
	Organization grn.GRN
	// Installation is the installation the instance is a copy of, and
	// its parent in every decision.
	Installation grn.GRN
	// CreatedBy is the member that created the record.
	CreatedBy grn.GRN
	CreatedAt time.Time
	// Name is what the organization calls the instance. Two may share
	// one; the GRN is what tells them apart.
	Name string
	// Description is what the organization says the instance is for,
	// empty where it says nothing.
	Description string
	// Kind is what the instance runs on, as the organization says it:
	// stored and shown, and never read.
	Kind string
}

// NewInstance records that installation has an instance under an
// identifier garam mints. createdBy is the member creating it, and
// details what it says about the instance; a name it leaves out is the
// identifier.
func NewInstance(installation *Installation, createdBy grn.GRN, details DetailsPatch) (*Instance, error) {
	b := make([]byte, identifierBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, errorx.Wrap(err, "instance: mint identifier", errorx.KindInternal)
	}
	g, err := InstanceGRN(installation.Organization, hex.EncodeToString(b))
	if err != nil {
		return nil, err
	}
	i := &Instance{
		GRN:          g,
		Organization: installation.Organization,
		Installation: installation.GRN,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		Name:         g.Identifier,
	}
	if details.name != nil {
		i.Name = *details.name
	}
	if details.description != nil {
		i.Description = *details.description
	}
	if details.kind != nil {
		i.Kind = *details.kind
	}
	return i, nil
}

// IsInstanceGRN reports whether g is an instance's GRN in the form
// InstanceGRN derives: the iam service's instance type, with no
// ancestors and no locator. A GRN of the instance type under another
// service is not.
func IsInstanceGRN(g grn.GRN) bool {
	return g.ServiceType == InstanceType && g == grn.New(g.Organization, InstanceType, g.Identifier)
}

// InstanceGRN names the instance of orgGRN under identifier. It is the
// one derivation of that name: creating builds the GRN it stores
// through this, and a route addressing an instance by its path segment
// builds the same one.
func InstanceGRN(orgGRN grn.GRN, identifier string) (grn.GRN, error) {
	g := grn.New(orgGRN.Identifier, InstanceType, identifier)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "instance: identifier", errorx.KindInvalidArgument)
	}
	return g, nil
}
