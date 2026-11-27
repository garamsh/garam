package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// The principal types an action names for an installation to be
// granted it (ADR-0192 decision 6): the installation, which an app
// key's token is decided as, and the instance, which sits in it.
const (
	installationEntity = "Iam::Installation"
	instanceEntity     = "Iam::Instance"
)

// ErrUnrequestable refuses an app whose record requests an action no
// installation may be granted. The record is written by garam, so the
// fault is garam's and not the installer's.
var ErrUnrequestable = errorx.New(errorx.KindInternal, "the app requests an action no installation may be granted")

// Requestable refuses an action an installation may not be granted: one
// declared does not hold, or one it holds without both the installation
// and the instance among its principals. declared is what the manifests
// declare, grn.Actions(). The floor of ADR-0192 decision 6 is held
// where the manifests are, which refuse an action of that property
// listing either.
func Requestable(action string, declared []grn.ActionInfo) error {
	for _, a := range declared {
		if a.Name != action {
			continue
		}
		if slices.Contains(a.Principals, installationEntity) && slices.Contains(a.Principals, instanceEntity) {
			return nil
		}
		return errorx.Wrap(ErrUnrequestable, action+" does not apply to an installation and its instances",
			errorx.KindInternal)
	}
	return errorx.Wrap(ErrUnrequestable, action+" is declared by no manifest", errorx.KindInternal)
}

// Requestable refuses the app when any action it requests is not
// requestable among declared.
func (a *App) Requestable(declared []grn.ActionInfo) error {
	for _, action := range a.RequestedActions {
		if err := Requestable(action, declared); err != nil {
			return err
		}
	}
	return nil
}

// Grant is one `permit` an install writes: its policy identifier, the
// name it is listed under and its Cedar text.
type Grant struct {
	ID   string
	Name string
	Text string
}

// grantPrefix opens the identifier of every policy an install writes.
const grantPrefix = "install-"

// Grants are the `permit`s installing a writes (ADR-0192 decision 4):
// one per requested action and per entity of the scope, its principal
// the installation and its resource in the organization or in the
// project. Each identifier is the installation's incarnation and the
// grant's place, so the same install names the same policies and a
// reinstall, another incarnation, names others. declared is as
// Requestable reads it.
func (i *Installation) Grants(a *App, declared []grn.ActionInfo) ([]Grant, error) {
	if a.GRN != i.App {
		return nil, errorx.New(errorx.KindInternal, "app: grants of "+a.GRN.String()+" for an installation of "+i.App.String())
	}
	if err := a.Requestable(declared); err != nil {
		return nil, err
	}
	scopes := []string{entityOf("Iam::Org", i.Organization)}
	if !i.Scope.IsOrganization() {
		scopes = scopes[:0]
		for _, p := range i.Scope.Projects() {
			scopes = append(scopes, entityOf("Iam::Project", p))
		}
	}
	grants := make([]Grant, 0, len(a.RequestedActions)*len(scopes))
	for _, action := range a.RequestedActions {
		cedarAction, err := cedarActionOf(action)
		if err != nil {
			return nil, err
		}
		for _, scope := range scopes {
			grants = append(grants, Grant{
				ID:   grantPrefix + i.Incarnation + "-" + strconv.Itoa(len(grants)),
				Name: a.Slug + " " + action,
				Text: fmt.Sprintf("permit (principal == %s, action == %s, resource in %s);",
					entityOf(installationEntity, i.GRN), cedarAction, scope),
			})
		}
	}
	return grants, nil
}

// entityOf is the Cedar literal of the entity of typ named g.
func entityOf(typ string, g grn.GRN) string {
	return typ + "::" + strconv.Quote(g.String())
}

// cedarActionOf is the Cedar literal of a wire-form action,
// `iam:org.read` being `Iam::Action::"org.read"`: its namespace is the
// one every type of its service is declared under.
func cedarActionOf(action string) (string, error) {
	service, id, ok := strings.Cut(action, ":")
	if !ok {
		return "", errorx.New(errorx.KindInternal, "app: action "+action+" names no service")
	}
	for _, info := range grn.Types(service) {
		namespace, _, ok := strings.Cut(info.Entity, "::")
		if ok {
			return namespace + "::Action::" + strconv.Quote(id), nil
		}
	}
	return "", errorx.New(errorx.KindInternal, "app: no namespace for "+service)
}
