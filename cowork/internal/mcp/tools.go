package mcp

import (
	"context"
	"log/slog"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

// The shapes the tools take and answer. Each field's description is
// what a client's model reads about it.
type (
	orgIn struct {
		Org string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
	}
	pageIn struct {
		Org  string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Page string `json:"page" jsonschema:"the page's id"`
	}
	searchIn struct {
		Org   string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Query string `json:"query" jsonschema:"words to find in page titles and messages; \"quoted phrases\" and -exclusions work in messages"`
	}
	createPageIn struct {
		Org    string         `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Title  string         `json:"title" jsonschema:"the page's title"`
		Parent string         `json:"parent,omitempty" jsonschema:"the id of the page to create it under; the top of the tree if empty"`
		Blocks []collab.Block `json:"blocks,omitempty" jsonschema:"the page's content, in order"`
	}
	editIn struct {
		Kind  collab.EditKind `json:"kind" jsonschema:"append, insert, replace or delete"`
		Index int             `json:"index,omitempty" jsonschema:"the block to insert before, replace or delete, from 0; unused for append"`
		Block *collab.Block   `json:"block,omitempty" jsonschema:"the block to append, insert or replace with"`
	}
	updatePageIn struct {
		Org   string   `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Page  string   `json:"page" jsonschema:"the page's id"`
		Title *string  `json:"title,omitempty" jsonschema:"a new title"`
		Edits []editIn `json:"edits,omitempty" jsonschema:"changes to the page's blocks, made in order; an index counts the blocks as the edits before it left them"`
	}
	channelIn struct {
		Org     string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Channel string `json:"channel" jsonschema:"the channel's id"`
		Before  string `json:"before,omitempty" jsonschema:"answer the messages before this message id; the latest if empty"`
	}
	postIn struct {
		Org     string   `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Channel string   `json:"channel" jsonschema:"the channel's id"`
		Body    string   `json:"body" jsonschema:"the message, in Markdown"`
		Thread  string   `json:"thread,omitempty" jsonschema:"the id of the message to reply to"`
		Pages   []string `json:"pages,omitempty" jsonschema:"ids of pages the message links"`
	}

	workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	workspacesOut struct {
		Workspaces []workspace `json:"workspaces"`
	}
	pageRef struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Parent string `json:"parent,omitempty"`
	}
	pagesOut struct {
		Pages []pageRef `json:"pages"`
	}
	pageOut struct {
		Page     pageRef        `json:"page"`
		Markdown string         `json:"markdown"`
		Blocks   []collab.Block `json:"blocks"`
	}
	channelRef struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Name string `json:"name,omitempty"`
	}
	channelsOut struct {
		Channels []channelRef `json:"channels"`
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
	messagesOut struct {
		Messages []message `json:"messages"`
	}
	searchOut struct {
		Pages    []pageRef `json:"pages"`
		Messages []message `json:"messages"`
	}
)

func toPageRef(p page.Page) pageRef { return pageRef{ID: p.ID, Title: p.Title, Parent: p.Parent} }

func toMessage(m channel.Message) message {
	return message{
		ID: m.ID, Channel: m.Channel, Thread: m.Thread, Author: m.Author.String(), Body: m.Body,
		Pages: m.Pages, At: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), Replies: m.Replies,
	}
}

func toMessages(ms []channel.Message) messagesOut {
	out := messagesOut{Messages: make([]message, len(ms))}
	for i, m := range ms {
		out.Messages[i] = toMessage(m)
	}
	return out
}

// tool registers one tool whose handler is the caller's: every one is
// handed the request's caller, and an error it answers reaches the
// client as the tool's error, not as data.
func tool[In, Out any](t *Tools, name, title, description string, readOnly bool, h func(context.Context, platform.Caller, In) (Out, error)) {
	annotations := &sdkmcp.ToolAnnotations{Title: title, ReadOnlyHint: readOnly}
	if !readOnly {
		destructive := false
		annotations.DestructiveHint = &destructive
	}
	sdkmcp.AddTool(t.server, &sdkmcp.Tool{Name: name, Title: title, Description: description, Annotations: annotations},
		func(ctx context.Context, _ *sdkmcp.CallToolRequest, in In) (*sdkmcp.CallToolResult, Out, error) {
			var zero Out
			c, err := callerOf(ctx)
			if err != nil {
				return nil, zero, err
			}
			out, err := h(ctx, c, in)
			if err != nil {
				return nil, zero, toolError(ctx, name, err)
			}
			return nil, out, nil
		})
}

// toolError is err as the client reads it. A refusal or a bad input
// says what it is; a failure of cowork's own names no cause, which is
// logged here, once, as writeError logs an HTTP one.
func toolError(ctx context.Context, name string, err error) error {
	switch kind := errorx.KindOf(err); kind {
	case errorx.KindUnavailable:
		slog.WarnContext(ctx, "cowork: tool failed", slog.String("tool", name), slog.String("error", err.Error()))
		return errorx.New(kind, name+": unavailable; try again")
	case errorx.KindInternal, errorx.KindUnknown, errorx.KindDataLoss:
		slog.ErrorContext(ctx, "cowork: tool failed", slog.String("tool", name), slog.String("error", err.Error()))
		return errorx.New(kind, name+": internal error")
	default:
		return errorx.Wrap(err, name, kind)
	}
}

func (t *Tools) register() {
	tool(t, "list_workspaces", "List workspaces",
		"The workspaces (organizations) you belong to. Every other tool takes one as org.", true, t.listWorkspaces)
	tool(t, "list_pages", "List pages",
		"Every page in the workspace you may read, as a tree: each names the page it sits under.", true, t.listPages)
	tool(t, "search", "Search",
		"Pages whose title holds the query, and messages that match it, in the channels you may read.", true, t.search)
	tool(t, "read_page", "Read a page",
		"A page's content, as Markdown and as its numbered blocks. update_page's edits name a block by its number.", true, t.readPage)
	tool(t, "create_page", "Create a page",
		"A new page, with its content if blocks are given. A block is a paragraph, heading (attrs.level 1-3), bulletList, orderedList, taskList (items, each with checked), blockquote, codeBlock (attrs.language) or horizontalRule.", false, t.createPage)
	tool(t, "update_page", "Update a page",
		"Renames a page and changes its blocks, in order. Everyone with the page open sees each edit as it is made.", false, t.updatePage)
	tool(t, "list_channels", "List channels",
		"The channels and direct messages you may read in the workspace.", true, t.listChannels)
	tool(t, "read_messages", "Read messages",
		"A channel's messages, oldest first: the latest, or those before a message.", true, t.readMessages)
	tool(t, "post_message", "Post a message",
		"Posts a message to a channel, or a reply in a thread. Everyone reading the channel sees it arrive.", false, t.postMessage)
	t.registerTasks()
	t.registerArtifacts()
}

func (t *Tools) listWorkspaces(ctx context.Context, c platform.Caller, _ struct{}) (workspacesOut, error) {
	orgs, err := t.platform.Organizations(ctx, c)
	if err != nil {
		return workspacesOut{}, err
	}
	out := workspacesOut{Workspaces: make([]workspace, len(orgs))}
	for i, o := range orgs {
		out.Workspaces[i] = workspace{ID: o.ID, Name: o.Name}
	}
	return out, nil
}

func (t *Tools) listPages(ctx context.Context, c platform.Caller, in orgIn) (pagesOut, error) {
	ps, err := t.pages.List(ctx, c, in.Org)
	if err != nil {
		return pagesOut{}, err
	}
	out := pagesOut{Pages: make([]pageRef, len(ps))}
	for i, p := range ps {
		out.Pages[i] = toPageRef(p)
	}
	return out, nil
}

// maxFound is the most pages a search answers.
const maxFound = 50

func (t *Tools) search(ctx context.Context, c platform.Caller, in searchIn) (searchOut, error) {
	ps, err := t.pages.List(ctx, c, in.Org)
	if err != nil {
		return searchOut{}, err
	}
	out := searchOut{Pages: []pageRef{}}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	for _, p := range ps {
		if len(out.Pages) < maxFound && q != "" && strings.Contains(strings.ToLower(p.Title), q) {
			out.Pages = append(out.Pages, toPageRef(p))
		}
	}
	ms, err := t.channels.Search(ctx, c, in.Org, in.Query)
	if err != nil {
		return searchOut{}, err
	}
	out.Messages = toMessages(ms).Messages
	return out, nil
}

// room answers the page's room, writable as the platform says the
// caller may write it, having read the page as the caller, which also
// proves it exists.
func (t *Tools) room(ctx context.Context, c platform.Caller, org, id string, write bool) (page.Page, collab.Room, error) {
	p, err := t.pages.Get(ctx, c, org, id)
	if err != nil {
		return page.Page{}, collab.Room{}, err
	}
	room := collab.Room{Org: org, Page: id}
	if write {
		if err := platform.Require(ctx, t.platform, c, org, platform.OnPage(platform.ActionPageWrite, id)); err != nil {
			return page.Page{}, collab.Room{}, err
		}
		room.Writable = true
	}
	return p, room, nil
}

func pageOf(p page.Page, blocks []collab.Block) pageOut {
	if blocks == nil {
		blocks = []collab.Block{}
	}
	return pageOut{Page: toPageRef(p), Markdown: collab.Markdown(blocks), Blocks: blocks}
}

func (t *Tools) readPage(ctx context.Context, c platform.Caller, in pageIn) (pageOut, error) {
	p, room, err := t.room(ctx, c, in.Org, in.Page, false)
	if err != nil {
		return pageOut{}, err
	}
	blocks, err := t.collab.Read(ctx, room)
	if err != nil {
		return pageOut{}, err
	}
	return pageOf(p, blocks), nil
}

func (t *Tools) createPage(ctx context.Context, c platform.Caller, in createPageIn) (pageOut, error) {
	p, err := t.pages.Create(ctx, c, in.Org, page.CreateInput{Title: in.Title, Parent: in.Parent})
	if err != nil {
		return pageOut{}, err
	}
	edits := make([]collab.Edit, len(in.Blocks))
	for i, b := range in.Blocks {
		edits[i] = collab.Edit{Kind: collab.EditAppend, Block: b}
	}
	// Creating the page was cowork:page-write on the organization;
	// writing its content is the same act on the page.
	_, room, err := t.room(ctx, c, in.Org, p.ID, true)
	if err != nil {
		return pageOut{}, err
	}
	blocks, err := t.collab.Apply(ctx, room, edits)
	if err != nil {
		return pageOut{}, err
	}
	return pageOf(p, blocks), nil
}

func (t *Tools) updatePage(ctx context.Context, c platform.Caller, in updatePageIn) (pageOut, error) {
	p, room, err := t.room(ctx, c, in.Org, in.Page, true)
	if err != nil {
		return pageOut{}, err
	}
	edits := make([]collab.Edit, len(in.Edits))
	for i, e := range in.Edits {
		edit := collab.Edit{Kind: e.Kind, Index: e.Index}
		switch {
		case e.Block != nil:
			edit.Block = *e.Block
		case e.Kind != collab.EditDelete:
			return pageOut{}, errorx.New(errorx.KindInvalidArgument, "an "+string(e.Kind)+" edit needs a block")
		}
		edits[i] = edit
	}
	if in.Title != nil {
		if p, err = t.pages.Update(ctx, c, in.Org, in.Page, page.UpdateInput{Title: in.Title}); err != nil {
			return pageOut{}, err
		}
	}
	blocks, err := t.collab.Apply(ctx, room, edits)
	if err != nil {
		return pageOut{}, err
	}
	return pageOf(p, blocks), nil
}

func (t *Tools) listChannels(ctx context.Context, c platform.Caller, in orgIn) (channelsOut, error) {
	cs, err := t.channels.List(ctx, c, in.Org)
	if err != nil {
		return channelsOut{}, err
	}
	out := channelsOut{Channels: make([]channelRef, len(cs))}
	for i, ch := range cs {
		out.Channels[i] = channelRef{ID: ch.ID, Kind: string(ch.Kind), Name: ch.Name}
	}
	return out, nil
}

func (t *Tools) readMessages(ctx context.Context, c platform.Caller, in channelIn) (messagesOut, error) {
	ms, err := t.channels.Messages(ctx, c, in.Org, in.Channel, in.Before)
	if err != nil {
		return messagesOut{}, err
	}
	return toMessages(ms), nil
}

func (t *Tools) postMessage(ctx context.Context, c platform.Caller, in postIn) (message, error) {
	m, err := t.channels.Post(ctx, c, in.Org, in.Channel, channel.PostInput{Body: in.Body, Thread: in.Thread, Pages: in.Pages})
	if err != nil {
		return message{}, err
	}
	return toMessage(m), nil
}
