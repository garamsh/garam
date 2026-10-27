package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiTask struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Assignee string   `json:"assignee"`
	Due      string   `json:"due"`
	Pages    []string `json:"pages"`
	Messages []string `json:"messages"`
}

func (s *stack) createTask(t *testing.T, who, body string) apiTask {
	t.Helper()
	status, got := s.call(t, who, http.MethodPost, "/orgs/acme/tasks", body)
	require.Equal(t, http.StatusCreated, status, got)
	var tk apiTask
	require.NoError(t, json.Unmarshal([]byte(got), &tk))
	return tk
}

func (s *stack) tasks(t *testing.T, who, query string) []apiTask {
	t.Helper()
	status, got := s.call(t, who, http.MethodGet, "/orgs/acme/tasks"+query, "")
	require.Equal(t, http.StatusOK, status, got)
	var out struct {
		Tasks []apiTask `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &out))
	return out.Tasks
}

// TestTasks_AreAssignedMovedAndCommentedOn keeps a task through its
// life: created for bob with a due date, moved to doing and done by
// bob, commented on, and kept across a read.
func TestTasks_AreAssignedMovedAndCommentedOn(t *testing.T) {
	s := newStack(t)
	tk := s.createTask(t, "ada", `{"title":"Write the launch post","assignee":"`+userGRN("bob")+`","due":"2026-11-02"}`)
	assert.Equal(t, "todo", tk.Status)
	assert.Equal(t, userGRN("bob"), tk.Assignee)

	status, body := s.call(t, "bob", http.MethodPatch, "/orgs/acme/tasks/"+tk.ID, `{"status":"doing"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = s.call(t, "bob", http.MethodPost, "/orgs/acme/tasks/"+tk.ID+"/comments", `{"body":"draft is up"}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = s.call(t, "bob", http.MethodPatch, "/orgs/acme/tasks/"+tk.ID, `{"status":"done","due":""}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = s.call(t, "ada", http.MethodGet, "/orgs/acme/tasks/"+tk.ID, "")
	require.Equal(t, http.StatusOK, status, body)
	var got apiTask
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	assert.Equal(t, "done", got.Status)
	assert.Empty(t, got.Due, "an empty due clears it")
	assert.Equal(t, userGRN("bob"), got.Assignee, "what the change did not set stays")

	status, body = s.call(t, "ada", http.MethodGet, "/orgs/acme/tasks/"+tk.ID+"/comments", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "draft is up")

	assert.Len(t, s.tasks(t, "ada", "?status=done"), 1)
	assert.Empty(t, s.tasks(t, "ada", "?status=todo"))
	assert.Len(t, s.tasks(t, "ada", "?assignee="+userGRN("bob")), 1)
}

// TestTasks_LinkPagesAndMessagesBothWays has a task link a page and a
// message, each found from the other side, and refuses a link to a
// message its writer may not read.
func TestTasks_LinkPagesAndMessagesBothWays(t *testing.T) {
	s := newStack(t)
	pg := s.createPage(t, "Launch")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	m := s.post(t, "ada", general.ID, `{"body":"we launch friday"}`)
	tk := s.createTask(t, "ada", `{"title":"Launch","pages":["`+pg+`"],"messages":["`+m.ID+`"]}`)
	assert.Equal(t, []string{pg}, tk.Pages)
	assert.Equal(t, []string{m.ID}, tk.Messages)

	byPage := s.tasks(t, "bob", "?page="+pg)
	require.Len(t, byPage, 1)
	assert.Equal(t, tk.ID, byPage[0].ID)
	byMessage := s.tasks(t, "bob", "?message="+m.ID)
	require.Len(t, byMessage, 1)

	plans := s.createChannel(t, "bob", `{"kind":"private","name":"plans"}`)
	secret := s.post(t, "bob", plans.ID, `{"body":"bob's plan"}`)
	status, body := s.call(t, "ada", http.MethodPost, "/orgs/acme/tasks", `{"title":"Peek","messages":["`+secret.ID+`"]}`)
	assert.Equal(t, http.StatusForbidden, status, body)
	status, body = s.call(t, "ada", http.MethodPatch, "/orgs/acme/tasks/"+tk.ID, `{"messages":["`+m.ID+`","`+secret.ID+`"]}`)
	assert.Equal(t, http.StatusForbidden, status, body)
}

// TestTasks_ThePlatformDecidesEachOne leaves a task the platform
// denies out of carol's list, and refuses carol a write it denies
// while she still reads.
func TestTasks_ThePlatformDecidesEachOne(t *testing.T) {
	s := newStack(t)
	open := s.createTask(t, "ada", `{"title":"open"}`)
	hidden := s.createTask(t, "ada", `{"title":"hidden"}`)
	s.platform.denyCheck("carol cowork:task-read@" + hidden.ID)
	s.platform.denyCheck("carol cowork:task-write@" + open.ID)

	listed := s.tasks(t, "carol", "")
	require.Len(t, listed, 1)
	assert.Equal(t, open.ID, listed[0].ID)

	status, _ := s.call(t, "carol", http.MethodGet, "/orgs/acme/tasks/"+hidden.ID, "")
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.call(t, "carol", http.MethodPatch, "/orgs/acme/tasks/"+open.ID, `{"status":"done"}`)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.call(t, "carol", http.MethodPost, "/orgs/acme/tasks/"+open.ID+"/comments", `{"body":"x"}`)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.call(t, "eve", http.MethodGet, "/orgs/acme/tasks", "")
	assert.Equal(t, http.StatusForbidden, status, "eve is in no workspace")
}

// TestMCP_AnAgentWorksATask has an agent, through the tools, find the
// task assigned to it, move it to doing with a comment, and finish
// it; a task it may not write is refused as the tool's error.
func TestMCP_AnAgentWorksATask(t *testing.T) {
	s := newStack(t)
	tk := s.createTask(t, "ada", `{"title":"Triage the inbox","assignee":"`+userGRN("bob")+`"}`)
	locked := s.createTask(t, "ada", `{"title":"Locked"}`)
	s.platform.denyCheck("bob cowork:task-write@" + locked.ID)
	bob := s.agent(t, "bob")

	var mine struct {
		Tasks []struct{ ID, Status string } `json:"tasks"`
	}
	bob.ok("list_tasks", map[string]any{"org": "acme", "assignee": "me"}, &mine)
	require.Len(t, mine.Tasks, 1)
	assert.Equal(t, tk.ID, mine.Tasks[0].ID)

	var moved struct {
		Task     struct{ Status string } `json:"task"`
		Comments []struct{ Body string } `json:"comments"`
	}
	bob.ok("update_task", map[string]any{"org": "acme", "task": tk.ID, "status": "doing", "comment": "starting now"}, &moved)
	assert.Equal(t, "doing", moved.Task.Status)
	require.Len(t, moved.Comments, 1)
	assert.Equal(t, "starting now", moved.Comments[0].Body)

	bob.ok("update_task", map[string]any{"org": "acme", "task": tk.ID, "status": "done"}, &moved)
	assert.Equal(t, "done", s.tasks(t, "ada", "?assignee="+userGRN("bob"))[0].Status, "ada reads what bob's agent did")

	var created struct{ ID, Assignee string }
	bob.ok("create_task", map[string]any{"org": "acme", "title": "Follow up", "assignee": "me"}, &created)
	assert.Equal(t, userGRN("bob"), created.Assignee)

	res := bob.call("update_task", map[string]any{"org": "acme", "task": locked.ID, "status": "done"})
	assert.True(t, res.IsError)
	_, body := s.call(t, "ada", http.MethodGet, "/orgs/acme/tasks/"+locked.ID, "")
	assert.Contains(t, body, `"status":"todo"`, "the refused change changed nothing")
}
