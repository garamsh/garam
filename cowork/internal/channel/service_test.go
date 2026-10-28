package channel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/channel/memory"
	fanoutmemory "github.com/garamsh/garam/cowork/internal/fanout/memory"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// policy allows every check but those deny names, as `action`,
// `action@id`, or either after a person's name and a space, and records
// whether it was asked at all.
type policy struct {
	deny  map[string]bool
	asked int
}

func (p *policy) Identify(context.Context, platform.Credential) (platform.Caller, error) {
	return platform.Caller{}, nil
}

func (p *policy) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return []platform.Organization{{ID: "acme", Name: "Acme"}}, nil
}

func (p *policy) Allowed(_ context.Context, c platform.Caller, org string, checks []platform.Check) ([]bool, error) {
	if org != "acme" {
		return nil, errorx.New(errorx.KindPermissionDenied, "not a member")
	}
	p.asked++
	out := make([]bool, len(checks))
	for i, ch := range checks {
		k := ch.Action
		if ch.ID != "" {
			k += "@" + ch.ID
		}
		out[i] = !p.deny[k] && !p.deny[c.GRN.Identifier+" "+k]
	}
	return out, nil
}

func person(name string) platform.Caller {
	return platform.Caller{GRN: grn.New(grn.RootOwner, grn.DefaultProject, "user", name)}
}

var (
	ada = person("ada")
	bob = person("bob")
	eve = person("eve")
	ctx = context.Background()
)

func newService(t *testing.T) (channel.Service, *policy, *fanoutmemory.Hub) {
	t.Helper()
	p := &policy{deny: map[string]bool{}}
	hub := fanoutmemory.New()
	return channel.NewService(memory.New(), p, hub), p, hub
}

func create(t *testing.T, s channel.Service, as platform.Caller, in channel.CreateInput) channel.Channel {
	t.Helper()
	c, err := s.Create(ctx, as, "acme", in)
	require.NoError(t, err)
	return c
}

func post(t *testing.T, s channel.Service, as platform.Caller, ch string, in channel.PostInput) channel.Message {
	t.Helper()
	m, err := s.Post(ctx, as, "acme", ch, in)
	require.NoError(t, err)
	return m
}

func bodies(ms []channel.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Body
	}
	return out
}

// TestPrivate_NarrowsToItsParticipants holds ADR-0128's list: bob,
// whom the platform allows, is refused a private channel bob is not
// on, and reads it once ada adds bob. Eve is refused leaving a list
// eve was never on.
func TestPrivate_NarrowsToItsParticipants(t *testing.T) {
	s, _, _ := newService(t)
	c := create(t, s, ada, channel.CreateInput{Kind: channel.KindPrivate, Name: "plans"})
	post(t, s, ada, c.ID, channel.PostInput{Body: "secret"})

	_, err := s.Messages(ctx, bob, "acme", c.ID, "")
	assert.ErrorIs(t, err, channel.ErrNotParticipant)
	listed, err := s.List(ctx, bob, "acme")
	require.NoError(t, err)
	assert.Empty(t, listed, "a private channel bob is not on is not listed for bob")

	require.NoError(t, s.AddMember(ctx, ada, "acme", c.ID, bob.GRN))
	ms, err := s.Messages(ctx, bob, "acme", c.ID, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"secret"}, bodies(ms))

	assert.ErrorIs(t, s.Leave(ctx, eve, "acme", c.ID), channel.ErrNotParticipant)
	require.NoError(t, s.Leave(ctx, bob, "acme", c.ID))
	_, err = s.Messages(ctx, bob, "acme", c.ID, "")
	assert.ErrorIs(t, err, channel.ErrNotParticipant)
}

// TestList_TheListGrantsNothingThePlatformDenies denies ada reading a
// private channel ada created and is on: ada is refused, and it is not
// listed. Bob, also on it and not denied, is the control.
func TestList_TheListGrantsNothingThePlatformDenies(t *testing.T) {
	s, p, _ := newService(t)
	c := create(t, s, ada, channel.CreateInput{Kind: channel.KindPrivate, Name: "plans", Members: []grn.GRN{bob.GRN}})
	p.deny["ada "+channel.ActionRead+"@"+c.ID] = true

	_, err := s.Messages(ctx, ada, "acme", c.ID, "")
	assert.ErrorIs(t, err, platform.ErrNotAllowed)
	listed, err := s.List(ctx, ada, "acme")
	require.NoError(t, err)
	assert.Empty(t, listed)

	_, err = s.Messages(ctx, bob, "acme", c.ID, "")
	assert.NoError(t, err)
}

// TestAccess_AsksThePlatformBeforeTheList holds that a caller the
// platform refuses is refused by the platform and not told about the
// list: eve, denied and not a participant, gets the platform's denial.
func TestAccess_AsksThePlatformBeforeTheList(t *testing.T) {
	s, p, _ := newService(t)
	c := create(t, s, ada, channel.CreateInput{Kind: channel.KindPrivate, Name: "plans"})
	_, err := s.Messages(ctx, eve, "acme", c.ID, "")
	require.ErrorIs(t, err, channel.ErrNotParticipant, "allowed by the platform, refused by the list")

	p.deny["eve "+channel.ActionRead+"@"+c.ID] = true
	_, err = s.Messages(ctx, eve, "acme", c.ID, "")
	assert.ErrorIs(t, err, platform.ErrNotAllowed)
}

// TestDirect_IsBetweenThePeopleItStartedWith starts a direct message
// with bob: both read it, eve does not, and nobody may add to it. One
// with nobody else in it is refused, and so is one given a name.
func TestDirect_IsBetweenThePeopleItStartedWith(t *testing.T) {
	s, _, _ := newService(t)
	dm := create(t, s, ada, channel.CreateInput{Kind: channel.KindDirect, Members: []grn.GRN{bob.GRN}})
	post(t, s, bob, dm.ID, channel.PostInput{Body: "hi ada"})

	ms, err := s.Messages(ctx, ada, "acme", dm.ID, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"hi ada"}, bodies(ms))
	_, err = s.Messages(ctx, eve, "acme", dm.ID, "")
	assert.ErrorIs(t, err, channel.ErrNotParticipant)
	assert.ErrorIs(t, s.AddMember(ctx, ada, "acme", dm.ID, eve.GRN), channel.ErrDirectIsFixed)

	_, err = s.Create(ctx, ada, "acme", channel.CreateInput{Kind: channel.KindDirect, Members: []grn.GRN{ada.GRN}})
	assert.ErrorIs(t, err, channel.ErrInvalidChannel)
	_, err = s.Create(ctx, ada, "acme", channel.CreateInput{Kind: channel.KindDirect, Name: "x", Members: []grn.GRN{bob.GRN}})
	assert.ErrorIs(t, err, channel.ErrInvalidChannel)
}

// TestPost_RepliesJoinTheFirstMessagesThread replies to a reply and
// reads one thread of three, while the channel lists only its first
// message, with two replies.
func TestPost_RepliesJoinTheFirstMessagesThread(t *testing.T) {
	s, _, _ := newService(t)
	c := create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "general"})
	root := post(t, s, ada, c.ID, channel.PostInput{Body: "question"})
	reply := post(t, s, bob, c.ID, channel.PostInput{Body: "answer", Thread: root.ID})
	post(t, s, ada, c.ID, channel.PostInput{Body: "thanks", Thread: reply.ID})

	thread, err := s.Thread(ctx, bob, "acme", reply.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"question", "answer", "thanks"}, bodies(thread))
	listed, err := s.Messages(ctx, bob, "acme", c.ID, "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, 2, listed[0].Replies)
}

// TestPost_LinksOnlyPagesTheAuthorMayRead links two pages and is
// refused when the author may not read one of them. The same post
// without it is the control.
func TestPost_LinksOnlyPagesTheAuthorMayRead(t *testing.T) {
	s, p, _ := newService(t)
	c := create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "general"})
	p.deny[platform.ActionPageRead+"@hidden"] = true

	_, err := s.Post(ctx, ada, "acme", c.ID, channel.PostInput{Body: "see", Pages: []string{"open", "hidden"}})
	assert.ErrorIs(t, err, channel.ErrPageNotReadable)
	m := post(t, s, ada, c.ID, channel.PostInput{Body: "see", Pages: []string{"open", "open"}})
	assert.Equal(t, []string{"open"}, m.Pages)

	linking, err := s.Linking(ctx, bob, "acme", "open")
	require.NoError(t, err)
	assert.Equal(t, []string{"see"}, bodies(linking))
}

// TestSearch_ReadsOnlyChannelsTheCallerMayRead searches for a word in
// a public channel and a private one bob is not on: bob finds the
// public message alone, and ada, on both, finds both.
func TestSearch_ReadsOnlyChannelsTheCallerMayRead(t *testing.T) {
	s, _, _ := newService(t)
	pub := create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "general"})
	priv := create(t, s, ada, channel.CreateInput{Kind: channel.KindPrivate, Name: "plans"})
	post(t, s, ada, pub.ID, channel.PostInput{Body: "launch on friday"})
	post(t, s, ada, priv.ID, channel.PostInput{Body: "launch price is secret"})

	found, err := s.Search(ctx, bob, "acme", "launch")
	require.NoError(t, err)
	assert.Equal(t, []string{"launch on friday"}, bodies(found))
	found, err = s.Search(ctx, ada, "acme", "Launch")
	require.NoError(t, err)
	assert.Len(t, found, 2)
}

type inbox struct{ got [][]byte }

func (i *inbox) Deliver(msg []byte) { i.got = append(i.got, msg) }

// TestListen_DeliversEveryMessagePosted listens on a channel and
// receives a message posted there, with an agent it mentions marked
// undelivered, and nothing from another channel.
func TestListen_DeliversEveryMessagePosted(t *testing.T) {
	s, _, _ := newService(t)
	general := create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "general"})
	other := create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "other"})
	box := &inbox{}
	leave, err := s.Listen(ctx, bob, "acme", general.ID, box)
	require.NoError(t, err)
	defer leave()

	agent := grn.New("acme", grn.DefaultProject, "agent", "helper")
	post(t, s, ada, other.ID, channel.PostInput{Body: "elsewhere"})
	post(t, s, ada, general.ID, channel.PostInput{Body: "@helper look", Mentions: []grn.GRN{bob.GRN, agent}})

	require.Len(t, box.got, 1)
	var got struct {
		Body                string   `json:"body"`
		Mentions            []string `json:"mentions"`
		UndeliveredMentions []string `json:"undeliveredMentions"`
	}
	require.NoError(t, json.Unmarshal(box.got[0], &got))
	assert.Equal(t, "@helper look", got.Body)
	assert.Equal(t, []string{bob.GRN.String(), agent.String()}, got.Mentions)
	assert.Equal(t, []string{agent.String()}, got.UndeliveredMentions)
}

// TestCreate_RefusesWhatItCannotKeep refuses a nameless channel, a
// name past 80 bytes, a name taken, and a kind that does not exist.
func TestCreate_RefusesWhatItCannotKeep(t *testing.T) {
	s, _, _ := newService(t)
	create(t, s, ada, channel.CreateInput{Kind: channel.KindPublic, Name: "general"})
	for name, in := range map[string]channel.CreateInput{
		"no name":   {Kind: channel.KindPublic},
		"long name": {Kind: channel.KindPrivate, Name: strings.Repeat("x", 81)},
		"no kind":   {Name: "x"},
	} {
		_, err := s.Create(ctx, ada, "acme", in)
		assert.ErrorIs(t, err, channel.ErrInvalidChannel, name)
	}
	_, err := s.Create(ctx, bob, "acme", channel.CreateInput{Kind: channel.KindPrivate, Name: "general"})
	assert.ErrorIs(t, err, channel.ErrNameTaken)
}
