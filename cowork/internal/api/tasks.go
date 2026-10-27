package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/cowork/internal/task"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
)

func taskToAPI(t task.Task) apigen.Task {
	assignee := ""
	if !t.Assignee.IsZero() {
		assignee = t.Assignee.String()
	}
	return apigen.Task{
		Id: t.ID, Grn: t.GRN().String(), Org: t.Org, Title: t.Title, Description: t.Description,
		Status: apigen.TaskStatus(t.Status), Assignee: assignee, Due: t.Due,
		Pages: nonNil(t.Pages), Messages: nonNil(t.Messages),
		CreatedBy: t.CreatedBy.String(), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

// parseAssignee reads an assignee a request names: the zero GRN for an
// empty one, which is no one.
func parseAssignee(s string) (grn.GRN, error) {
	if s == "" {
		return grn.GRN{}, nil
	}
	g, err := grn.Parse(s)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "assignee", errorx.KindInvalidArgument)
	}
	return g, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// ListTasks implements apigen.ServerInterface.
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, params apigen.ListTasksParams) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	assignee, err := parseAssignee(deref(params.Assignee))
	if err != nil {
		writeError(w, r, err)
		return
	}
	ts, err := s.tasks.List(r.Context(), c, org, task.Filter{
		Status: task.Status(deref(params.Status)), Assignee: assignee, Page: deref(params.Page), Message: deref(params.Message),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.TaskList{Tasks: make([]apigen.Task, len(ts))}
	for i, t := range ts {
		out.Tasks[i] = taskToAPI(t)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateTask implements apigen.ServerInterface.
func (s *Server) CreateTask(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.TaskCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	assignee, err := parseAssignee(deref(body.Assignee))
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := s.tasks.Create(r.Context(), c, org, task.CreateInput{
		Title: body.Title, Description: deref(body.Description), Status: task.Status(deref(body.Status)),
		Assignee: assignee, Due: deref(body.Due), Pages: deref(body.Pages), Messages: deref(body.Messages),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, taskToAPI(t))
}

// GetTask implements apigen.ServerInterface.
func (s *Server) GetTask(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.TaskPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := s.tasks.Get(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, taskToAPI(t))
}

// UpdateTask implements apigen.ServerInterface.
func (s *Server) UpdateTask(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.TaskPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.TaskUpdate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	in := task.UpdateInput{Title: body.Title, Description: body.Description, Due: body.Due, Pages: body.Pages, Messages: body.Messages}
	if body.Status != nil {
		st := task.Status(*body.Status)
		in.Status = &st
	}
	if body.Assignee != nil {
		g, err := parseAssignee(*body.Assignee)
		if err != nil {
			writeError(w, r, err)
			return
		}
		in.Assignee = &g
	}
	t, err := s.tasks.Update(r.Context(), c, org, id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, taskToAPI(t))
}

func commentToAPI(cm task.Comment) apigen.TaskComment {
	return apigen.TaskComment{Id: cm.ID, Task: cm.Task, Author: cm.Author.String(), Body: cm.Body, CreatedAt: cm.CreatedAt}
}

// ListTaskComments implements apigen.ServerInterface.
func (s *Server) ListTaskComments(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.TaskPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	cs, err := s.tasks.Comments(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.TaskCommentList{Comments: make([]apigen.TaskComment, len(cs))}
	for i, cm := range cs {
		out.Comments[i] = commentToAPI(cm)
	}
	writeJSON(w, http.StatusOK, out)
}

// PostTaskComment implements apigen.ServerInterface.
func (s *Server) PostTaskComment(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.TaskPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.TaskCommentPost
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	cm, err := s.tasks.Comment(r.Context(), c, org, id, body.Body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, commentToAPI(cm))
}
