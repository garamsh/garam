package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
)

// newOrgsCommand lists organizations and saves the default one.
func newOrgsCommand() *cobra.Command {
	flags := &userFlags{}
	orgs := &cobra.Command{Use: "orgs", Short: "Organizations you are a member of"}
	flags.bind(orgs)
	orgs.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List your organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			client, err := c.signedIn(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := client.ListOrgsWithResponse(cmd.Context(), nil)
			if err != nil {
				return errorx.Wrap(err, "list orgs", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			list := *resp.JSON200
			return c.render(list, []string{"ORG", "NAME", "CREATED"}, func() [][]string {
				rows := make([][]string, 0, len(list))
				for _, o := range list {
					rows = append(rows, []string{lastSegment(o.Grn), o.Name, stamp(o.CreatedAt)})
				}
				return rows
			})
		},
	}, &cobra.Command{
		Use:   "use <org>",
		Short: "Save the organization commands act in when --org is not passed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			client, err := c.signedIn(cmd.Context())
			if err != nil {
				return err
			}
			// Read first, so what is saved is an organization the session
			// can reach rather than a typo every later command trips on.
			resp, err := client.GetOrgWithResponse(cmd.Context(), args[0])
			if err != nil {
				return errorx.Wrap(err, "get org", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			org := lastSegment(resp.JSON200.Grn)
			if err := c.store.update(func(file *credentialsFile) (bool, error) {
				entry := file.Endpoints[c.endpoint]
				if entry == nil {
					entry = &endpointCredentials{}
					file.Endpoints[c.endpoint] = entry
				}
				entry.Org = org
				return true, nil
			}); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Commands on %s now act in %s.\n", c.endpoint, org)
			return nil
		},
	})
	return orgs
}

// newOperatorsCommand lists an organization's operators.
func newOperatorsCommand() *cobra.Command {
	flags := &userFlags{}
	operators := &cobra.Command{Use: "operators", Short: "An organization's operators"}
	flags.bind(operators)
	operators.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the organization's operators",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			org, err := c.org()
			if err != nil {
				return err
			}
			client, err := c.signedIn(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := client.ListOperatorsWithResponse(cmd.Context(), org)
			if err != nil {
				return errorx.Wrap(err, "list operators", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			list := *resp.JSON200
			return c.render(list, []string{"OPERATOR", "NAME", "DESCRIPTION", "CREATED"}, func() [][]string {
				rows := make([][]string, 0, len(list))
				for _, o := range list {
					rows = append(rows, []string{lastSegment(o.Grn), o.Name, o.Description, stamp(o.CreatedAt)})
				}
				return rows
			})
		},
	})
	return operators
}

// newAgentsCommand lists and reads an organization's agents.
func newAgentsCommand() *cobra.Command {
	flags := &userFlags{}
	agents := &cobra.Command{Use: "agents", Short: "An organization's agents"}
	flags.bind(agents)
	header := []string{"AGENT", "PROVISIONING", "RUNTIME", "CREATED"}
	row := func(a apigen.Agent) []string {
		return []string{lastSegment(a.Grn), string(a.ProvisioningState), string(a.RuntimeState), stamp(a.CreatedAt)}
	}
	agents.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the organization's agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			org, err := c.org()
			if err != nil {
				return err
			}
			client, err := c.signedIn(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := client.ListAgentsWithResponse(cmd.Context(), org, nil)
			if err != nil {
				return errorx.Wrap(err, "list agents", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			list := *resp.JSON200
			return c.render(list, header, func() [][]string {
				rows := make([][]string, 0, len(list))
				for _, a := range list {
					rows = append(rows, row(a))
				}
				return rows
			})
		},
	}, &cobra.Command{
		Use:   "get <agent>",
		Short: "Show one agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			org, err := c.org()
			if err != nil {
				return err
			}
			client, err := c.signedIn(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := client.GetAgentWithResponse(cmd.Context(), org, args[0])
			if err != nil {
				return errorx.Wrap(err, "get agent", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			return c.render(resp.JSON200, header, func() [][]string {
				return [][]string{row(*resp.JSON200)}
			})
		},
	})
	return agents
}

// stamp is a time as a table shows it: UTC, to the second.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
