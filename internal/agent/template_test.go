package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
)

func TestNewTemplate_MintsAnIdentityOfItsOwnInTheOrganization(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	createdBy := grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")
	content := agent.TemplateContent{
		Name:        "Researcher",
		Description: "Reads papers",
		Values:      agent.Values{"model": "haiku"},
	}

	tpl, err := agent.NewTemplate(orgGRN, content, createdBy)
	require.NoError(t, err)
	assert.Equal(t, "acme", tpl.GRN.Organization)
	assert.Equal(t, agent.ServiceTypeTemplate, tpl.GRN.ServiceType)
	assert.Len(t, tpl.GRN.Identifier, 16, "eight random bytes, hex-encoded")
	require.NoError(t, tpl.GRN.Validate())
	assert.Equal(t, orgGRN, tpl.OrgGRN)
	assert.Equal(t, content, tpl.TemplateContent)
	assert.Equal(t, createdBy, tpl.CreatedBy)
	assert.Equal(t, tpl.CreatedAt, tpl.UpdatedAt)

	// A template names no agent: the identity a definition mints is
	// its own, so no agent can share one with the template it copied.
	other, err := agent.NewTemplate(orgGRN, content, createdBy)
	require.NoError(t, err)
	assert.NotEqual(t, tpl.GRN, other.GRN)
	assert.NotEqual(t, agent.ServiceType, tpl.GRN.ServiceType)
}

func TestNewTemplate_HoldsAnEmptyPayloadRatherThanNone(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	tpl, err := agent.NewTemplate(orgGRN, agent.TemplateContent{Name: "Blank"},
		grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada"))
	require.NoError(t, err)
	assert.NotNil(t, tpl.TemplateContent.Values)
	assert.Empty(t, tpl.TemplateContent.Values)
}
