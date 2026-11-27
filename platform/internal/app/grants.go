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

// Floor is the actions no app may request (ADR-0192 decision 6, as
// ADR-0193 holds it): those that grant, delegate or change membership,
// roles, invitations, policies, consent, a principal's credentials or
// the organization itself. Who may hold one is the schema's to say;
// that no app requests one is held here. A name no manifest declares
// yet stands for the action that will.
var Floor = []string{
	"iam:org.update", "iam:org.delete", "iam:org.invite",
	"iam:group.create", "iam:group.update", "iam:group.delete",
	"iam:group.member-add", "iam:group.member-remove",
	"iam:project.create", "iam:project.update", "iam:project.delete",
	"iam:project.member-add", "iam:project.member-remove",
	"iam:policy.create", "iam:policy.update", "iam:policy.attach",
	"iam:policy.detach", "iam:policy.delete", "iam:permit.write",
	"iam:consent.grant", "iam:consent.withdraw",
	"iam:operator.register", "iam:operator.update", "iam:operator.delete",
	"iam:operator.reissue", "iam:operator.delegate",
	"iam:agent.enroll",
	"iam:app.create", "iam:app.update", "iam:app.delete",
	"iam:installation.create", "iam:installation.update", "iam:installation.delete",
	"iam:instance.register", "iam:instance.update", "iam:instance.reissue",
	"iam:instance.delete",
	"iam:bot.host", "iam:bot.place",
}

// Requestable refuses an action an installation may not be granted:
// one of the Floor, one declared does not hold, or one it holds
// without both the installation and the instance among its
// principals. declared is what the manifests declare, grn.Actions().
func Requestable(action string, declared []grn.ActionInfo) error {
	if slices.Contains(Floor, action) {
		return errorx.Wrap(ErrUnrequestable, action+" is of the floor no app may request", errorx.KindInternal)
	}
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

// GrantPrefix opens the identifier of every policy an install writes,
// which an install's purge is limited to.
const GrantPrefix = "install-"

// Grants are the `permit`s installing a writes (ADR-0192 decision 4):
// one per requested action and per entity of the scope, its principal
// `in` the installation and its resource in the organization or in the
// project. `in` and not `==`: an instance's token is decided with the
// instance, whose parent is its installation, so each instance holds
// what the installation does (decision 6). Each identifier is the
// installation's incarnation and the grant's place, so the same install
// names the same policies and a reinstall, another incarnation, names
// others. declared is as Requestable reads it.
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
				ID:   GrantPrefix + i.Incarnation + "-" + strconv.Itoa(len(grants)),
				Name: a.Slug + " " + action,
				Text: fmt.Sprintf("permit (principal in %s, action == %s, resource in %s);",
					entityOf(installationEntity, i.GRN), cedarAction, scope),
			})
		}
	}
	return grants, nil
}

// ErrBotHostingNotRequested refuses bot hosting for an installation
// whose app does not ask to host bots.
var ErrBotHostingNotRequested = errorx.New(errorx.KindInvalidArgument, "the app does not ask to host bots")

// BotHostGrant is the `permit` of iam:bot.host on the installation
// itself that its owner's grant of bot hosting writes (ADR-0192
// decision 10). Its identifier is under GrantPrefix and names the
// incarnation, so a reinstall's purge and an uninstall's take it, and
// it names the installation alone, so no instance holds it of its own.
// A refusal is ErrBotHostingNotRequested.
func (i *Installation) BotHostGrant(a *App) (Grant, error) {
	if a.GRN != i.App {
		return Grant{}, errorx.New(errorx.KindInternal, "app: bot hosting of "+a.GRN.String()+" for an installation of "+i.App.String())
	}
	if !a.RequestsBotHosting {
		return Grant{}, ErrBotHostingNotRequested
	}
	installation := entityOf(installationEntity, i.GRN)
	return Grant{
		ID:   GrantPrefix + i.Incarnation + "-bot-host",
		Name: a.Slug + " iam:bot.host",
		Text: fmt.Sprintf("permit (principal == %s, action == Iam::Action::%q, resource == %s);",
			installation, "bot.host", installation),
	}, nil
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
