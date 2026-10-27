package mcp

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/cowork/internal/task"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
)

type (
	listTasksIn struct {
		Org      string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Status   string `json:"status,omitempty" jsonschema:"todo, doing or done"`
		Assignee string `json:"assignee,omitempty" jsonschema:"the GRN of the person or agent assigned; me for the caller"`
		Page     string `json:"page,omitempty" jsonschema:"the id of a page the tasks link"`
		Message  string `json:"message,omitempty" jsonschema:"the id of a message the tasks link"`
	}
	taskIn struct {
		Org  string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Task string `json:"task" jsonschema:"the task's id"`
	}
	createTaskIn struct {
		Org         string   `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Title       string   `json:"title" jsonschema:"what is to be done"`
		Description string   `json:"description,omitempty" jsonschema:"more about it, in Markdown"`
		Status      string   `json:"status,omitempty" jsonschema:"todo, doing or done; todo if empty"`
		Assignee    string   `json:"assignee,omitempty" jsonschema:"the GRN of the person or agent to assign; me for the caller"`
		Due         string   `json:"due,omitempty" jsonschema:"the due date, as YYYY-MM-DD"`
		Pages       []string `json:"pages,omitempty" jsonschema:"ids of pages the task links"`
		Messages    []string `json:"messages,omitempty" jsonschema:"ids of messages the task links"`
	}
	updateTaskIn struct {
		Org         string    `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Task        string    `json:"task" jsonschema:"the task's id"`
		Title       *string   `json:"title,omitempty" jsonschema:"a new title"`
		Description *string   `json:"description,omitempty" jsonschema:"a new description"`
		Status      *string   `json:"status,omitempty" jsonschema:"todo, doing or done"`
		Assignee    *string   `json:"assignee,omitempty" jsonschema:"the GRN to assign; me for the caller; empty for no one"`
		Due         *string   `json:"due,omitempty" jsonschema:"a due date as YYYY-MM-DD; empty for none"`
		Pages       *[]string `json:"pages,omitempty" jsonschema:"the pages the task links, replacing those it did"`
		Messages    *[]string `json:"messages,omitempty" jsonschema:"the messages the task links, replacing those it did"`
		Comment     string    `json:"comment,omitempty" jsonschema:"a comment to add, saying what changed and why"`
	}
	commentIn struct {
		Org  string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Task string `json:"task" jsonschema:"the task's id"`
		Body string `json:"body" jsonschema:"the comment, in Markdown"`
	}

	taskOut struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description,omitempty"`
		Status      string   `json:"status"`
		Assignee    string   `json:"assignee,omitempty"`
		Due         string   `json:"due,omitempty"`
		Pages       []string `json:"pages,omitempty"`
		Messages    []string `json:"messages,omitempty"`
		CreatedBy   string   `json:"createdBy"`
		UpdatedAt   string   `json:"updatedAt"`
	}
	tasksOut struct {
		Tasks []taskOut `json:"tasks"`
	}
	comment struct {
		ID     string `json:"id"`
		Author string `json:"author"`
		Body   string `json:"body"`
		At     string `json:"at"`
	}
	taskDetailOut struct {
		Task     taskOut   `json:"task"`
		Comments []comment `json:"comments"`
	}
)

const timeLayout = "2006-01-02T15:04:05Z"

func toTask(t task.Task) taskOut {
	out := taskOut{
		ID: t.ID, Title: t.Title, Description: t.Description, Status: string(t.Status), Due: t.Due,
		Pages: t.Pages, Messages: t.Messages, CreatedBy: t.CreatedBy.String(), UpdatedAt: t.UpdatedAt.UTC().Format(timeLayout),
	}
	if !t.Assignee.IsZero() {
		out.Assignee = t.Assignee.String()
	}
	return out
}

func toComment(c task.Comment) comment {
	return comment{ID: c.ID, Author: c.Author.String(), Body: c.Body, At: c.CreatedAt.UTC().Format(timeLayout)}
}

// assigneeOf reads an assignee a tool names: me is the caller, and an
// empty one is no one.
func assigneeOf(c platform.Caller, s string) (grn.GRN, error) {
	switch s {
	case "":
		return grn.GRN{}, nil
	case "me":
		return c.GRN, nil
	}
	g, err := grn.Parse(s)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "assignee", errorx.KindInvalidArgument)
	}
	return g, nil
}

func (t *Tools) registerTasks() {
	tool(t, "list_tasks", "List tasks",
		"The tasks you may read, newest first, narrowed by status, assignee, or a page or message they link.", true, t.listTasks)
	tool(t, "read_task", "Read a task",
		"A task with its comments.", true, t.readTask)
	tool(t, "create_task", "Create a task",
		"A new task, assigned to a person or an agent if you name one, linked to pages and messages.", false, t.createTask)
	tool(t, "update_task", "Update a task",
		"Changes what you set on a task, its status among them, and adds a comment if you give one.", false, t.updateTask)
	tool(t, "comment_task", "Comment on a task",
		"Adds a comment to a task.", false, t.commentTask)
}

func (t *Tools) listTasks(ctx context.Context, c platform.Caller, in listTasksIn) (tasksOut, error) {
	assignee, err := assigneeOf(c, in.Assignee)
	if err != nil {
		return tasksOut{}, err
	}
	ts, err := t.tasks.List(ctx, c, in.Org, task.Filter{Status: task.Status(in.Status), Assignee: assignee, Page: in.Page, Message: in.Message})
	if err != nil {
		return tasksOut{}, err
	}
	out := tasksOut{Tasks: make([]taskOut, len(ts))}
	for i, tk := range ts {
		out.Tasks[i] = toTask(tk)
	}
	return out, nil
}

func (t *Tools) detail(ctx context.Context, c platform.Caller, org string, tk task.Task) (taskDetailOut, error) {
	cs, err := t.tasks.Comments(ctx, c, org, tk.ID)
	if err != nil {
		return taskDetailOut{}, err
	}
	out := taskDetailOut{Task: toTask(tk), Comments: make([]comment, len(cs))}
	for i, cm := range cs {
		out.Comments[i] = toComment(cm)
	}
	return out, nil
}

func (t *Tools) readTask(ctx context.Context, c platform.Caller, in taskIn) (taskDetailOut, error) {
	tk, err := t.tasks.Get(ctx, c, in.Org, in.Task)
	if err != nil {
		return taskDetailOut{}, err
	}
	return t.detail(ctx, c, in.Org, tk)
}

func (t *Tools) createTask(ctx context.Context, c platform.Caller, in createTaskIn) (taskOut, error) {
	assignee, err := assigneeOf(c, in.Assignee)
	if err != nil {
		return taskOut{}, err
	}
	tk, err := t.tasks.Create(ctx, c, in.Org, task.CreateInput{
		Title: in.Title, Description: in.Description, Status: task.Status(in.Status), Assignee: assignee,
		Due: in.Due, Pages: in.Pages, Messages: in.Messages,
	})
	if err != nil {
		return taskOut{}, err
	}
	return toTask(tk), nil
}

func (t *Tools) updateTask(ctx context.Context, c platform.Caller, in updateTaskIn) (taskDetailOut, error) {
	update := task.UpdateInput{Title: in.Title, Description: in.Description, Due: in.Due, Pages: in.Pages, Messages: in.Messages}
	if in.Status != nil {
		st := task.Status(*in.Status)
		update.Status = &st
	}
	if in.Assignee != nil {
		g, err := assigneeOf(c, *in.Assignee)
		if err != nil {
			return taskDetailOut{}, err
		}
		update.Assignee = &g
	}
	tk, err := t.tasks.Update(ctx, c, in.Org, in.Task, update)
	if err != nil {
		return taskDetailOut{}, err
	}
	if in.Comment != "" {
		if _, err := t.tasks.Comment(ctx, c, in.Org, in.Task, in.Comment); err != nil {
			return taskDetailOut{}, err
		}
	}
	return t.detail(ctx, c, in.Org, tk)
}

func (t *Tools) commentTask(ctx context.Context, c platform.Caller, in commentIn) (comment, error) {
	cm, err := t.tasks.Comment(ctx, c, in.Org, in.Task, in.Body)
	if err != nil {
		return comment{}, err
	}
	return toComment(cm), nil
}
