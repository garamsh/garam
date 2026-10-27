package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// The client commands that work in a workspace. Each calls the MCP
// tool of its name, so the CLI does what an AI client may, as the same
// caller, and -o json prints what the tool answered.

type (
	pageRef struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Parent string `json:"parent,omitempty"`
	}
	message struct {
		ID      string   `json:"id"`
		Channel string   `json:"channel"`
		Thread  string   `json:"thread,omitempty"`
		Author  string   `json:"author"`
		Body    string   `json:"body"`
		Pages   []string `json:"pages,omitempty"`
		At      string   `json:"at"`
		Replies int      `json:"replies,omitempty"`
	}
	pageContent struct {
		Page     pageRef          `json:"page"`
		Markdown string           `json:"markdown"`
		Blocks   []map[string]any `json:"blocks"`
	}
)

func pageRows(ps []pageRef) [][]string {
	rows := make([][]string, len(ps))
	for i, p := range ps {
		rows[i] = []string{p.ID, p.Title, p.Parent}
	}
	return rows
}

// firstLine is a message's body as one table cell.
func firstLine(body string) string {
	line, _, more := strings.Cut(body, "\n")
	if more {
		line += " …"
	}
	return line
}

func messageRows(ms []message) [][]string {
	rows := make([][]string, len(ms))
	for i, m := range ms {
		replies := ""
		if m.Replies > 0 {
			replies = strconv.Itoa(m.Replies)
		}
		rows[i] = []string{m.ID, m.At, m.Author, replies, firstLine(m.Body)}
	}
	return rows
}

var messageHeader = []string{"ID", "AT", "AUTHOR", "REPLIES", "BODY"}

// inOrg runs a command that acts in the workspace.
func inOrg(cmd *cobra.Command, flags *clientFlags, run func(*cobra.Command, *client, string, []string) error) *cobra.Command {
	return clientCommand(cmd, flags, func(cmd *cobra.Command, c *client, args []string) error {
		org, err := c.org()
		if err != nil {
			return err
		}
		return run(cmd, c, org, args)
	})
}

func newWorkspacesCommand(flags *clientFlags) *cobra.Command {
	return clientCommand(&cobra.Command{
		Use:   "workspaces",
		Short: "List the workspaces you belong to",
		Args:  cobra.NoArgs,
	}, flags, func(cmd *cobra.Command, c *client, _ []string) error {
		var out struct {
			Workspaces []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"workspaces"`
		}
		if err := c.call(cmd.Context(), "list_workspaces", map[string]any{}, &out); err != nil {
			return err
		}
		rows := make([][]string, len(out.Workspaces))
		for i, w := range out.Workspaces {
			rows[i] = []string{w.ID, w.Name}
		}
		return c.render(out, []string{"ID", "NAME"}, rows)
	})
}

func newPagesCommand(flags *clientFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "pages", Short: "List, read and create pages"}
	cmd.AddCommand(
		inOrg(&cobra.Command{Use: "list", Short: "List the pages you may read", Args: cobra.NoArgs}, flags,
			func(cmd *cobra.Command, c *client, org string, _ []string) error {
				var out struct {
					Pages []pageRef `json:"pages"`
				}
				if err := c.call(cmd.Context(), "list_pages", map[string]any{"org": org}, &out); err != nil {
					return err
				}
				return c.render(out, []string{"ID", "TITLE", "PARENT"}, pageRows(out.Pages))
			}),
		inOrg(&cobra.Command{Use: "read <page>", Short: "Print a page as Markdown", Args: cobra.ExactArgs(1)}, flags,
			func(cmd *cobra.Command, c *client, org string, args []string) error {
				var out pageContent
				if err := c.call(cmd.Context(), "read_page", map[string]any{"org": org, "page": args[0]}, &out); err != nil {
					return err
				}
				if c.flags.output == outputJSON {
					return c.render(out, nil, nil)
				}
				_, err := fmt.Fprint(c.out, out.Markdown)
				return err
			}),
		newPagesCreateCommand(flags),
	)
	return cmd
}

func newPagesCreateCommand(flags *clientFlags) *cobra.Command {
	var title, parent string
	var paragraphs []string
	cmd := inOrg(&cobra.Command{Use: "create", Short: "Create a page", Args: cobra.NoArgs}, flags,
		func(cmd *cobra.Command, c *client, org string, _ []string) error {
			blocks := make([]map[string]any, len(paragraphs))
			for i, p := range paragraphs {
				blocks[i] = map[string]any{"type": "paragraph", "text": p}
			}
			var out pageContent
			args := map[string]any{"org": org, "title": title, "blocks": blocks}
			if parent != "" {
				args["parent"] = parent
			}
			if err := c.call(cmd.Context(), "create_page", args, &out); err != nil {
				return err
			}
			return c.render(out, []string{"ID", "TITLE", "PARENT"}, pageRows([]pageRef{out.Page}))
		})
	cmd.Flags().StringVar(&title, "title", "", "the page's title")
	cmd.Flags().StringVar(&parent, "parent", "", "the page to create it under")
	cmd.Flags().StringArrayVar(&paragraphs, "paragraph", nil, "a paragraph of its content; repeat for more")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newChannelsCommand(flags *clientFlags) *cobra.Command {
	return inOrg(&cobra.Command{Use: "channels", Short: "List the channels and direct messages you may read", Args: cobra.NoArgs}, flags,
		func(cmd *cobra.Command, c *client, org string, _ []string) error {
			var out struct {
				Channels []struct {
					ID   string `json:"id"`
					Kind string `json:"kind"`
					Name string `json:"name,omitempty"`
				} `json:"channels"`
			}
			if err := c.call(cmd.Context(), "list_channels", map[string]any{"org": org}, &out); err != nil {
				return err
			}
			rows := make([][]string, len(out.Channels))
			for i, ch := range out.Channels {
				rows[i] = []string{ch.ID, ch.Kind, ch.Name}
			}
			return c.render(out, []string{"ID", "KIND", "NAME"}, rows)
		})
}

func newMessagesCommand(flags *clientFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "messages", Short: "Read and post messages"}
	var before string
	list := inOrg(&cobra.Command{Use: "list <channel>", Short: "List a channel's messages, oldest first", Args: cobra.ExactArgs(1)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			var out struct {
				Messages []message `json:"messages"`
			}
			in := map[string]any{"org": org, "channel": args[0]}
			if before != "" {
				in["before"] = before
			}
			if err := c.call(cmd.Context(), "read_messages", in, &out); err != nil {
				return err
			}
			return c.render(out, messageHeader, messageRows(out.Messages))
		})
	list.Flags().StringVar(&before, "before", "", "list the messages before this one")

	var thread string
	post := inOrg(&cobra.Command{Use: "post <channel> <body>", Short: "Post a message, or a reply with --thread", Args: cobra.ExactArgs(2)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			var out message
			in := map[string]any{"org": org, "channel": args[0], "body": args[1]}
			if thread != "" {
				in["thread"] = thread
			}
			if err := c.call(cmd.Context(), "post_message", in, &out); err != nil {
				return err
			}
			return c.render(out, messageHeader, messageRows([]message{out}))
		})
	post.Flags().StringVar(&thread, "thread", "", "the message to reply to")
	cmd.AddCommand(list, post)
	return cmd
}

func newSearchCommand(flags *clientFlags) *cobra.Command {
	return inOrg(&cobra.Command{Use: "search <query>", Short: "Find pages and messages", Args: cobra.ExactArgs(1)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			var out struct {
				Pages    []pageRef `json:"pages"`
				Messages []message `json:"messages"`
			}
			if err := c.call(cmd.Context(), "search", map[string]any{"org": org, "query": args[0]}, &out); err != nil {
				return err
			}
			var rows [][]string
			for _, p := range out.Pages {
				rows = append(rows, []string{"page", p.ID, p.Title})
			}
			for _, m := range out.Messages {
				rows = append(rows, []string{"message", m.ID, firstLine(m.Body)})
			}
			return c.render(out, []string{"TYPE", "ID", "TEXT"}, rows)
		})
}
