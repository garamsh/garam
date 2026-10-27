// Package mcp serves cowork's tools to AI clients over the Model
// Context Protocol (ADR-0127): reading and editing pages, and reading
// and posting messages, each as the caller who presented the request's
// credential. A tool asks the services the HTTP API asks, so the
// platform decides every call exactly as it decides the API's.
package mcp

import (
	"context"
	"net/http"
	"runtime/debug"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// Path is where the tools are served, in the protocol's streamable
// HTTP transport.
const Path = "/mcp"

// Tools are cowork's tools over its services.
type Tools struct {
	pages    page.Service
	collab   collab.Service
	channels channel.Service
	platform platform.Platform
	server   *sdkmcp.Server
}

// New answers the tools over cowork's services.
func New(pages page.Service, collabs collab.Service, channels channel.Service, p platform.Platform) *Tools {
	t := &Tools{pages: pages, collab: collabs, channels: channels, platform: p}
	t.server = sdkmcp.NewServer(&sdkmcp.Implementation{Name: "cowork", Version: version()}, &sdkmcp.ServerOptions{
		Instructions: instructions,
	})
	t.register()
	return t
}

// version names the build serving the tools: its commit where the
// build recorded one.
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "dev"
}

// instructions is what a client is told about the tools as a whole.
const instructions = `cowork is where people and agents write pages together, message each other, and keep the record of their work.
Every tool but list_workspaces takes the workspace (org) it acts in; list_workspaces answers the ones you belong to.
Pages are read as Markdown and as numbered blocks. update_page changes blocks by their index, and its edits reach everyone editing the page live.`

// callerKey carries the request's caller to the tool it calls.
type callerKey struct{}

// callerOf answers the caller the request presented.
func callerOf(ctx context.Context) (platform.Caller, error) {
	c, ok := ctx.Value(callerKey{}).(platform.Caller)
	if !ok {
		return platform.Caller{}, errorx.New(errorx.KindUnauthenticated, "no caller")
	}
	return c, nil
}

// Handler serves the tools. identify answers the request's caller, as
// the HTTP API identifies one, and a request it refuses is answered
// 401 before any tool runs. The transport is stateless: every request
// is identified on its own, so each tool call is its own caller's.
func (t *Tools) Handler(identify func(*http.Request) (platform.Caller, error), refuse func(http.ResponseWriter, *http.Request, error)) http.Handler {
	h := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return t.server }, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := identify(r)
		if err != nil {
			refuse(w, r, err)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}
