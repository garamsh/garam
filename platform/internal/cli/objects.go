package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// newOrgsCommand saves the default organization; the derived orgs
// commands sit beside it (ADR-0122).
func newOrgsCommand() *cobra.Command {
	flags := &userFlags{}
	orgs := &cobra.Command{Use: "orgs", Short: "Organizations you are a member of"}
	flags.bind(orgs)
	orgs.AddCommand(&cobra.Command{
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

// newAgentsCommand lists, reads and renames an organization's agents.
func newAgentsCommand() *cobra.Command {
	flags := &userFlags{}
	agents := &cobra.Command{Use: "agents", Short: "An organization's agents"}
	flags.bind(agents)
	header := []string{"AGENT", "NAME", "PROVISIONING", "RUNTIME", "CREATED"}
	row := func(a apigen.Agent) []string {
		name := "-"
		if a.Name != nil {
			name = *a.Name
		}
		return []string{lastSegment(a.Grn), name, string(a.ProvisioningState), string(a.RuntimeState), stamp(a.CreatedAt)}
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
	}, newAgentsRenameCommand(flags, header, row))
	return agents
}

// newAgentsRenameCommand names an agent, or clears its name with
// --clear (ADR-0119).
func newAgentsRenameCommand(flags *userFlags, header []string, row func(apigen.Agent) []string) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "rename <agent> [name]",
		Short: "Name an agent, or clear its name with --clear",
		Args: func(cmd *cobra.Command, args []string) error {
			if clear {
				return cobra.ExactArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
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
			var name *string
			if !clear {
				name = &args[1]
			}
			resp, err := client.RenameAgentWithResponse(cmd.Context(), org, args[0], apigen.AgentPatch{Name: name})
			if err != nil {
				return errorx.Wrap(err, "rename agent", errorx.KindUnavailable)
			}
			if resp.JSON200 == nil {
				return apiFailure(resp.HTTPResponse, resp.Body)
			}
			return c.render(resp.JSON200, header, func() [][]string {
				return [][]string{row(*resp.JSON200)}
			})
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the name, so the agent is shown by its identifier")
	return cmd
}

// stamp is a time as a table shows it: UTC, to the second.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
