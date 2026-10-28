package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/garam-cli/internal/session"
)

// defaultCoworkEndpoint is the cowork a command talks to where neither
// --cowork-endpoint nor COWORK_ENDPOINT names one.
const defaultCoworkEndpoint = "https://cowork.garam.sh"

// coworkFlag binds --cowork-endpoint to cmd and every command under
// it, and answers where the value lands.
func coworkFlag(cmd *cobra.Command) *string {
	var v string
	cmd.PersistentFlags().StringVar(&v, "cowork-endpoint", "",
		"the cowork to talk to (default $"+string(envCoworkEndpoint)+", or "+defaultCoworkEndpoint+")")
	return &v
}

// coworkServer answers the cowork a run talks to: --cowork-endpoint,
// else COWORK_ENDPOINT, else cowork.garam.sh. The session's tokens
// cross it, so it is held to the platform endpoint's terms.
func coworkServer(flag *string) apiServer {
	return func(*userCommand) (string, error) {
		raw := *flag
		if raw == "" {
			raw = envOr(envCoworkEndpoint, defaultCoworkEndpoint)
		}
		return session.ParseEndpoint(raw)
	}
}

// newCoworkCommand is the noun cowork's commands sit under (ADR-0133):
// those derived from cowork's API, added beside it, and the three that
// call cowork's MCP tools because the API does not carry a page's
// content.
func newCoworkCommand() (*cobra.Command, apiServer) {
	flags := &userFlags{}
	cowork := &cobra.Command{Use: "cowork", Short: "Pages, messages, tasks and artifacts in cowork"}
	flags.bind(cowork)
	server := coworkServer(coworkFlag(cowork))
	pages := &cobra.Command{Use: "pages", Short: "The API's pages"}
	pages.AddCommand(newCoworkPagesReadCommand(flags, server), newCoworkPagesCreateCommand(flags, server))
	cowork.AddCommand(pages, newCoworkSearchCommand(flags, server))
	return cowork, server
}

// coworkRun is one run of a command calling cowork's tools: the user
// command, the cowork it calls and the workspace it acts in.
func coworkRun(cmd *cobra.Command, flags *userFlags, server apiServer) (*userCommand, string, string, error) {
	c, err := newUserCommand(cmd, flags)
	if err != nil {
		return nil, "", "", err
	}
	base, err := server(c)
	if err != nil {
		return nil, "", "", err
	}
	org, err := c.org()
	if err != nil {
		return nil, "", "", err
	}
	return c, base, org, nil
}

type coworkPageRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Parent string `json:"parent,omitempty"`
}

type coworkPage struct {
	Page     coworkPageRef    `json:"page"`
	Markdown string           `json:"markdown"`
	Blocks   []map[string]any `json:"blocks"`
}

func newCoworkPagesReadCommand(flags *userFlags, server apiServer) *cobra.Command {
	return &cobra.Command{
		Use:   "read <page>",
		Short: "Print a page's content as Markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, base, org, err := coworkRun(cmd, flags, server)
			if err != nil {
				return err
			}
			var out coworkPage
			if err := c.callTool(cmd.Context(), base, "read_page", map[string]any{"org": org, "page": args[0]}, &out); err != nil {
				return err
			}
			if c.flags.output == outputJSON {
				return c.render(out, nil, nil)
			}
			_, err = fmt.Fprint(c.out, out.Markdown)
			return err
		},
	}
}

func newCoworkPagesCreateCommand(flags *userFlags, server apiServer) *cobra.Command {
	var title, parent string
	var paragraphs []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a page, with its content",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, base, org, err := coworkRun(cmd, flags, server)
			if err != nil {
				return err
			}
			blocks := make([]map[string]any, len(paragraphs))
			for i, p := range paragraphs {
				blocks[i] = map[string]any{"type": "paragraph", "text": p}
			}
			in := map[string]any{"org": org, "title": title, "blocks": blocks}
			if parent != "" {
				in["parent"] = parent
			}
			var out coworkPage
			if err := c.callTool(cmd.Context(), base, "create_page", in, &out); err != nil {
				return err
			}
			p := out.Page
			return c.render(out, []string{"ID", "TITLE", "PARENT"}, func() [][]string {
				return [][]string{{p.ID, p.Title, p.Parent}}
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "the page's title")
	cmd.Flags().StringVar(&parent, "parent", "", "the page to create it under")
	cmd.Flags().StringArrayVar(&paragraphs, "paragraph", nil, "a paragraph of its content; repeat for more")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newCoworkSearchCommand(flags *userFlags, server apiServer) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Find pages by title and messages by content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, base, org, err := coworkRun(cmd, flags, server)
			if err != nil {
				return err
			}
			var out struct {
				Pages    []coworkPageRef `json:"pages"`
				Messages []struct {
					ID   string `json:"id"`
					Body string `json:"body"`
				} `json:"messages"`
			}
			if err := c.callTool(cmd.Context(), base, "search", map[string]any{"org": org, "query": args[0]}, &out); err != nil {
				return err
			}
			return c.render(out, []string{"TYPE", "ID", "TEXT"}, func() [][]string {
				var rows [][]string
				for _, p := range out.Pages {
					rows = append(rows, []string{"page", p.ID, p.Title})
				}
				for _, m := range out.Messages {
					line, _, more := strings.Cut(m.Body, "\n")
					if more {
						line += " …"
					}
					rows = append(rows, []string{"message", m.ID, line})
				}
				return rows
			})
		},
	}
}
