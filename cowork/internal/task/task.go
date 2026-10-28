// Package task is cowork's record of work: an organization's tasks,
// each with an assignee, a status, a due date, comments, and links to
// pages and messages (ADR-0127). Who may read and write them is the
// platform's to decide.
package task

import (
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ServiceType is a task's GRN service type, the kind the platform's
// permission checks name it by.
const ServiceType = "cowork-task"

// The platform's actions on tasks: on the organization, to list and
// create them, and on a task, to read and change it.
const (
	ActionRead  = "cowork:task-read"
	ActionWrite = "cowork:task-write"
)

// Status is where a task stands, and a board's column.
type Status string

const (
	StatusTodo  Status = "todo"
	StatusDoing Status = "doing"
	StatusDone  Status = "done"
)

// Statuses are every status, in a board's order.
var Statuses = []Status{StatusTodo, StatusDoing, StatusDone}

func (s Status) valid() bool { return s == StatusTodo || s == StatusDoing || s == StatusDone }

// DateLayout is how a due date is written: a day, in no time zone.
const DateLayout = time.DateOnly

// Task is one task of an organization. Assignee is the zero GRN for a
// task assigned to no one, and Due empty for one without a due date.
type Task struct {
	ID          string
	Org         string
	Title       string
	Description string
	Status      Status
	Assignee    grn.GRN
	Due         string
	CreatedBy   grn.GRN
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Pages and Messages are the IDs of what the task links.
	Pages    []string
	Messages []string
}

// GRN is the task's resource name under its organization.
func (t Task) GRN() grn.GRN {
	return grn.New(t.Org, grn.DefaultProject, ServiceType, t.ID)
}

// Comment is one comment on a task.
type Comment struct {
	ID        string
	Org       string
	Task      string
	Author    grn.GRN
	Body      string
	CreatedAt time.Time
}

// CreateInput is a new task. Status is todo where it is empty.
type CreateInput struct {
	Title       string
	Description string
	Status      Status
	Assignee    grn.GRN
	Due         string
	Pages       []string
	Messages    []string
}

// UpdateInput changes what it sets and leaves the rest. Assignee set
// to the zero GRN takes the assignee off, and Due set to an empty
// string the due date; Pages and Messages replace what the task links.
type UpdateInput struct {
	Title       *string
	Description *string
	Status      *Status
	Assignee    *grn.GRN
	Due         *string
	Pages       *[]string
	Messages    *[]string
}

// Filter narrows a listing to the tasks matching every field it sets.
type Filter struct {
	Status   Status
	Assignee grn.GRN
	// Page and Message list the tasks that link them.
	Page    string
	Message string
}

// Limits on what a caller sends.
const (
	maxTitle       = 500
	maxDescription = 40000
	maxBody        = 40000
	maxLinks       = 20
	// maxListed is the most tasks one listing answers.
	maxListed = 500
)

var (
	ErrTaskNotFound = errorx.New(errorx.KindNotFound, "task not found")
	ErrInvalidTask  = errorx.New(errorx.KindInvalidArgument,
		"a task has a title of 1 to 500 bytes, a description of at most 40000, a status of todo, doing or done, a due date as YYYY-MM-DD, and at most 20 pages and 20 messages")
	ErrInvalidAssignee = errorx.New(errorx.KindInvalidArgument, "an assignee is a person's or an agent's GRN")
	ErrInvalidComment  = errorx.New(errorx.KindInvalidArgument, "a comment has a body of 1 to 40000 bytes")
)
