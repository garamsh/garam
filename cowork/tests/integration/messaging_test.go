package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiChannel struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type apiMessage struct {
	ID                  string   `json:"id"`
	Channel             string   `json:"channel"`
	Thread              string   `json:"thread"`
	Author              string   `json:"author"`
	Body                string   `json:"body"`
	Mentions            []string `json:"mentions"`
	UndeliveredMentions []string `json:"undeliveredMentions"`
	Pages               []string `json:"pages"`
	Replies             int      `json:"replies"`
}

func userGRN(who string) string { return "grn:root:default:user:" + who }

func (s *stack) createChannel(t *testing.T, who, body string) apiChannel {
	t.Helper()
	status, got := s.call(t, who, http.MethodPost, "/orgs/acme/channels", body)
	require.Equal(t, http.StatusCreated, status, got)
	var c apiChannel
	require.NoError(t, json.Unmarshal([]byte(got), &c))
	return c
}

func (s *stack) post(t *testing.T, who, channel, body string) apiMessage {
	t.Helper()
	status, got := s.call(t, who, http.MethodPost, "/orgs/acme/channels/"+channel+"/messages", body)
	require.Equal(t, http.StatusCreated, status, got)
	var m apiMessage
	require.NoError(t, json.Unmarshal([]byte(got), &m))
	return m
}

func (s *stack) messages(t *testing.T, who, path string) []apiMessage {
	t.Helper()
	status, got := s.call(t, who, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, got)
	var out struct {
		Messages []apiMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &out))
	return out.Messages
}

func bodiesOf(ms []apiMessage) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Body
	}
	return out
}

// listen opens a channel's live WebSocket as who, and answers each
// message that arrives on the returned channel.
func (s *stack) listen(t *testing.T, who, channel string) <-chan apiMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(s.url, "http") + "/orgs/acme/channels/" + channel + "/live"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {"garam_session=" + who}}})
	require.NoError(t, err)
	out := make(chan apiMessage, 16)
	go func() {
		defer close(out)
		for {
			_, raw, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m apiMessage
			if json.Unmarshal(raw, &m) == nil {
				out <- m
			}
		}
	}()
	return out
}

func next(t *testing.T, ch <-chan apiMessage) apiMessage {
	t.Helper()
	select {
	case m, ok := <-ch:
		require.True(t, ok, "the live connection closed")
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message arrived live")
	}
	return apiMessage{}
}

// TestMessaging_ArrivesLiveForEveryListener has ada and bob listen on a
// channel; bob's post reaches both as it is posted, with no read, and
// a post in another channel reaches neither.
func TestMessaging_ArrivesLiveForEveryListener(t *testing.T) {
	s := newStack(t)
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	other := s.createChannel(t, "ada", `{"kind":"public","name":"other"}`)
	adaLive, bobLive := s.listen(t, "ada", general.ID), s.listen(t, "bob", general.ID)

	s.post(t, "ada", other.ID, `{"body":"elsewhere"}`)
	posted := s.post(t, "bob", general.ID, `{"body":"hello everyone"}`)
	for _, live := range []<-chan apiMessage{adaLive, bobLive} {
		got := next(t, live)
		assert.Equal(t, posted.ID, got.ID)
		assert.Equal(t, "hello everyone", got.Body)
		assert.Equal(t, userGRN("bob"), got.Author)
	}
}

// TestMessaging_PrivateNarrowsAndTheListGrantsNothing holds ADR-0128
// over the wired server: bob is refused a private channel, its
// messages and its live feed until ada adds bob, after which bob reads;
// and once the platform denies bob reading it, being on the list does
// not let bob in.
func TestMessaging_PrivateNarrowsAndTheListGrantsNothing(t *testing.T) {
	s := newStack(t)
	plans := s.createChannel(t, "ada", `{"kind":"private","name":"plans"}`)
	s.post(t, "ada", plans.ID, `{"body":"secret"}`)
	path := "/orgs/acme/channels/" + plans.ID + "/messages"

	status, _ := s.call(t, "bob", http.MethodGet, path, "")
	assert.Equal(t, http.StatusForbidden, status, "not on the list")
	status, body := s.call(t, "ada", http.MethodPost, "/orgs/acme/channels/"+plans.ID+"/members",
		`{"member":"`+userGRN("bob")+`"}`)
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, []string{"secret"}, bodiesOf(s.messages(t, "bob", path)))

	s.platform.denyCheck("bob cowork:channel-read@" + plans.ID)
	status, _ = s.call(t, "bob", http.MethodGet, path, "")
	assert.Equal(t, http.StatusForbidden, status, "on the list, denied by the platform")
	assert.Equal(t, []string{"secret"}, bodiesOf(s.messages(t, "ada", path)), "ada, not denied, still reads")
}

// TestMessaging_DirectMessagesAreBetweenTheirPeople starts a direct
// message between ada and bob: both read it, carol, a member of the
// organization the platform allows, is refused by the list, and nobody
// can add to it.
func TestMessaging_DirectMessagesAreBetweenTheirPeople(t *testing.T) {
	s := newStack(t)
	dm := s.createChannel(t, "ada", `{"kind":"direct","members":["`+userGRN("bob")+`"]}`)
	assert.ElementsMatch(t, []string{userGRN("ada"), userGRN("bob")}, dm.Members)
	s.post(t, "bob", dm.ID, `{"body":"hi ada"}`)
	path := "/orgs/acme/channels/" + dm.ID + "/messages"
	assert.Equal(t, []string{"hi ada"}, bodiesOf(s.messages(t, "ada", path)))

	status, body := s.call(t, "carol", http.MethodGet, path, "")
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, body, "not a participant", "refused by the list, not by the platform")
	status, _ = s.call(t, "ada", http.MethodPost, "/orgs/acme/channels/"+dm.ID+"/members", `{"member":"`+userGRN("carol")+`"}`)
	assert.Equal(t, http.StatusConflict, status)
}

// TestMessaging_ThreadsHoldTheirReplies replies to a message and to
// the reply, and reads one thread of three while the channel lists the
// first message alone, with its two replies.
func TestMessaging_ThreadsHoldTheirReplies(t *testing.T) {
	s := newStack(t)
	c := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	root := s.post(t, "ada", c.ID, `{"body":"question"}`)
	reply := s.post(t, "bob", c.ID, `{"body":"answer","thread":"`+root.ID+`"}`)
	again := s.post(t, "ada", c.ID, `{"body":"thanks","thread":"`+reply.ID+`"}`)
	assert.Equal(t, root.ID, again.Thread, "a reply to a reply joins the first message's thread")

	assert.Equal(t, []string{"question", "answer", "thanks"}, bodiesOf(s.messages(t, "bob", "/orgs/acme/messages/"+reply.ID+"/thread")))
	listed := s.messages(t, "bob", "/orgs/acme/channels/"+c.ID+"/messages")
	require.Len(t, listed, 1)
	assert.Equal(t, 2, listed[0].Replies)
}

// TestMessaging_SearchFindsOnlyWhatTheCallerMayRead searches for a word
// posted in a public channel and in a private one bob is not on: bob
// finds one, ada finds both, and a phrase narrows to one.
func TestMessaging_SearchFindsOnlyWhatTheCallerMayRead(t *testing.T) {
	s := newStack(t)
	pub := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	priv := s.createChannel(t, "ada", `{"kind":"private","name":"plans"}`)
	s.post(t, "ada", pub.ID, `{"body":"the launch is on friday"}`)
	s.post(t, "ada", priv.ID, `{"body":"launch pricing stays secret"}`)

	assert.Equal(t, []string{"the launch is on friday"}, bodiesOf(s.messages(t, "bob", "/orgs/acme/messages?q=launch")))
	assert.Len(t, s.messages(t, "ada", "/orgs/acme/messages?q=Launch"), 2)
	assert.Equal(t, []string{"launch pricing stays secret"}, bodiesOf(s.messages(t, "ada", `/orgs/acme/messages?q=%22launch+pricing%22`)))
}

// TestMessaging_LinksPagesBothWays posts a message that links a page
// and mentions bob and an agent, then reads it from the page's side.
// The agent is marked undelivered. Linking a page the author may not
// read is refused, which the allowed link is the control for.
func TestMessaging_LinksPagesBothWays(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "spec")
	hidden := s.createPage(t, "hidden")
	c := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	agent := "grn:acme:default:agent:helper"

	m := s.post(t, "ada", c.ID, `{"body":"see the spec","pages":["`+pageID+`"],"mentions":["`+userGRN("bob")+`","`+agent+`"]}`)
	assert.Equal(t, []string{agent}, m.UndeliveredMentions)
	linking := s.messages(t, "bob", "/orgs/acme/pages/"+pageID+"/messages")
	require.Len(t, linking, 1)
	assert.Equal(t, m.ID, linking[0].ID)

	s.platform.denyCheck("ada cowork:page-read@" + hidden)
	status, _ := s.call(t, "ada", http.MethodPost, "/orgs/acme/channels/"+c.ID+"/messages", `{"body":"x","pages":["`+hidden+`"]}`)
	assert.Equal(t, http.StatusForbidden, status)
}

// TestMessaging_RefusesANonMember answers eve, signed in and in no
// organization, 403 on listing channels. Bob listing them is the
// control.
func TestMessaging_RefusesANonMember(t *testing.T) {
	s := newStack(t)
	s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	status, _ := s.call(t, "bob", http.MethodGet, "/orgs/acme/channels", "")
	require.Equal(t, http.StatusOK, status)
	status, _ = s.call(t, "eve", http.MethodGet, "/orgs/acme/channels", "")
	assert.Equal(t, http.StatusForbidden, status)
}
