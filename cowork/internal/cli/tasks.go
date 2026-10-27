package cli

import (
	"github.com/spf13/cobra"
)

type taskRef struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Assignee    string   `json:"assignee,omitempty"`
	Due         string   `json:"due,omitempty"`
	Pages       []string `json:"pages,omitempty"`
	Messages    []string `json:"messages,omitempty"`
}

var taskHeader = []string{"ID", "STATUS", "DUE", "ASSIGNEE", "TITLE"}

func taskRows(ts []taskRef) [][]string {
	rows := make([][]string, len(ts))
	for i, t := range ts {
		rows[i] = []string{t.ID, t.Status, t.Due, t.Assignee, t.Title}
	}
	return rows
}

func newTasksCommand(flags *clientFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "tasks", Short: "List, create and work tasks"}
	cmd.AddCommand(newTasksListCommand(flags), newTasksShowCommand(flags), newTasksCreateCommand(flags),
		newTasksUpdateCommand(flags), newTasksCommentCommand(flags))
	return cmd
}

func newTasksListCommand(flags *clientFlags) *cobra.Command {
	var status, assignee, page, message string
	cmd := inOrg(&cobra.Command{Use: "list", Short: "List the tasks you may read, newest first", Args: cobra.NoArgs}, flags,
		func(cmd *cobra.Command, c *client, org string, _ []string) error {
			in := map[string]any{"org": org}
			for k, v := range map[string]string{"status": status, "assignee": assignee, "page": page, "message": message} {
				if v != "" {
					in[k] = v
				}
			}
			var out struct {
				Tasks []taskRef `json:"tasks"`
			}
			if err := c.call(cmd.Context(), "list_tasks", in, &out); err != nil {
				return err
			}
			return c.render(out, taskHeader, taskRows(out.Tasks))
		})
	cmd.Flags().StringVar(&status, "status", "", "todo, doing or done")
	cmd.Flags().StringVar(&assignee, "assignee", "", "a person's or an agent's GRN, or me")
	cmd.Flags().StringVar(&page, "page", "", "only tasks linking this page")
	cmd.Flags().StringVar(&message, "message", "", "only tasks linking this message")
	return cmd
}

func newTasksShowCommand(flags *clientFlags) *cobra.Command {
	return inOrg(&cobra.Command{Use: "show <task>", Short: "Show a task and its comments", Args: cobra.ExactArgs(1)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			var out struct {
				Task     taskRef `json:"task"`
				Comments []struct {
					ID     string `json:"id"`
					Author string `json:"author"`
					Body   string `json:"body"`
					At     string `json:"at"`
				} `json:"comments"`
			}
			if err := c.call(cmd.Context(), "read_task", map[string]any{"org": org, "task": args[0]}, &out); err != nil {
				return err
			}
			rows := taskRows([]taskRef{out.Task})
			for _, cm := range out.Comments {
				rows = append(rows, []string{cm.ID, "comment", cm.At, cm.Author, firstLine(cm.Body)})
			}
			return c.render(out, taskHeader, rows)
		})
}

// taskFields are the flags that set a task's fields, on create and
// update alike.
type taskFields struct {
	title, description, status, assignee, due string
	pages, messages                           []string
}

func (f *taskFields) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.title, "title", "", "what is to be done")
	cmd.Flags().StringVar(&f.description, "description", "", "more about it")
	cmd.Flags().StringVar(&f.status, "status", "", "todo, doing or done")
	cmd.Flags().StringVar(&f.assignee, "assignee", "", "a person's or an agent's GRN, or me; empty on update for no one")
	cmd.Flags().StringVar(&f.due, "due", "", "the due date, as YYYY-MM-DD; empty on update for none")
	cmd.Flags().StringArrayVar(&f.pages, "page", nil, "a page it links; repeat for more")
	cmd.Flags().StringArrayVar(&f.messages, "message", nil, "a message it links; repeat for more")
}

// set puts into in each field whose flag the command was given.
func (f *taskFields) set(cmd *cobra.Command, in map[string]any) {
	for name, v := range map[string]any{
		"title": f.title, "description": f.description, "status": f.status, "assignee": f.assignee, "due": f.due,
		"page": f.pages, "message": f.messages,
	} {
		if cmd.Flags().Changed(name) {
			key := name
			if name == "page" || name == "message" {
				key += "s"
			}
			in[key] = v
		}
	}
}

func newTasksCreateCommand(flags *clientFlags) *cobra.Command {
	f := &taskFields{}
	cmd := inOrg(&cobra.Command{Use: "create", Short: "Create a task", Args: cobra.NoArgs}, flags,
		func(cmd *cobra.Command, c *client, org string, _ []string) error {
			in := map[string]any{"org": org}
			f.set(cmd, in)
			var out taskRef
			if err := c.call(cmd.Context(), "create_task", in, &out); err != nil {
				return err
			}
			return c.render(out, taskHeader, taskRows([]taskRef{out}))
		})
	f.bind(cmd)
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTasksUpdateCommand(flags *clientFlags) *cobra.Command {
	f := &taskFields{}
	var note string
	cmd := inOrg(&cobra.Command{Use: "update <task>", Short: "Change what the flags set on a task", Args: cobra.ExactArgs(1)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			in := map[string]any{"org": org, "task": args[0]}
			f.set(cmd, in)
			if note != "" {
				in["comment"] = note
			}
			var out struct {
				Task taskRef `json:"task"`
			}
			if err := c.call(cmd.Context(), "update_task", in, &out); err != nil {
				return err
			}
			return c.render(out, taskHeader, taskRows([]taskRef{out.Task}))
		})
	f.bind(cmd)
	cmd.Flags().StringVar(&note, "comment", "", "a comment saying what changed and why")
	return cmd
}

func newTasksCommentCommand(flags *clientFlags) *cobra.Command {
	return inOrg(&cobra.Command{Use: "comment <task> <body>", Short: "Comment on a task", Args: cobra.ExactArgs(2)}, flags,
		func(cmd *cobra.Command, c *client, org string, args []string) error {
			var out struct {
				ID     string `json:"id"`
				Author string `json:"author"`
				Body   string `json:"body"`
				At     string `json:"at"`
			}
			if err := c.call(cmd.Context(), "comment_task", map[string]any{"org": org, "task": args[0], "body": args[1]}, &out); err != nil {
				return err
			}
			return c.render(out, []string{"ID", "AT", "AUTHOR", "BODY"}, [][]string{{out.ID, out.At, out.Author, firstLine(out.Body)}})
		})
}
