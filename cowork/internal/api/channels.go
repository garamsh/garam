package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
)

func grnStrings(gs []grn.GRN) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.String()
	}
	return out
}

// parseGRNs reads the GRNs a request names, refusing one that does not
// parse.
func parseGRNs(ss []string) ([]grn.GRN, error) {
	out := make([]grn.GRN, 0, len(ss))
	for _, s := range ss {
		g, err := grn.Parse(s)
		if err != nil {
			return nil, errorx.Wrap(err, s, errorx.KindInvalidArgument)
		}
		out = append(out, g)
	}
	return out, nil
}

func channelToAPI(c channel.Channel) apigen.Channel {
	return apigen.Channel{
		Id: c.ID, Grn: c.GRN().String(), Org: c.Org, Kind: apigen.ChannelKind(c.Kind), Name: c.Name,
		CreatedBy: c.CreatedBy.String(), CreatedAt: c.CreatedAt, Members: grnStrings(c.Members),
	}
}

func messageToAPI(m channel.Message) apigen.Message {
	return apigen.Message{
		Id: m.ID, Channel: m.Channel, Thread: m.Thread, Author: m.Author.String(), Body: m.Body,
		Mentions: grnStrings(m.Mentions), UndeliveredMentions: grnStrings(m.UndeliveredMentions()),
		Pages: append([]string{}, m.Pages...), CreatedAt: m.CreatedAt, Replies: m.Replies,
	}
}

func messagesToAPI(ms []channel.Message) apigen.MessageList {
	out := apigen.MessageList{Messages: make([]apigen.Message, len(ms))}
	for i, m := range ms {
		out.Messages[i] = messageToAPI(m)
	}
	return out
}

// ListChannels implements apigen.ServerInterface.
func (s *Server) ListChannels(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	cs, err := s.channels.List(r.Context(), c, org)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.ChannelList{Channels: make([]apigen.Channel, len(cs))}
	for i, ch := range cs {
		out.Channels[i] = channelToAPI(ch)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateChannel implements apigen.ServerInterface.
func (s *Server) CreateChannel(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.ChannelCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	in := channel.CreateInput{Kind: channel.Kind(body.Kind)}
	if body.Name != nil {
		in.Name = *body.Name
	}
	if body.Members != nil {
		if in.Members, err = parseGRNs(*body.Members); err != nil {
			writeError(w, r, err)
			return
		}
	}
	ch, err := s.channels.Create(r.Context(), c, org, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, channelToAPI(ch))
}

// GetChannel implements apigen.ServerInterface.
func (s *Server) GetChannel(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ChannelPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ch, err := s.channels.Get(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, channelToAPI(ch))
}

// AddChannelMember implements apigen.ServerInterface.
func (s *Server) AddChannelMember(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ChannelPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.MemberAdd
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	member, err := grn.Parse(body.Member)
	if err != nil {
		writeError(w, r, errorx.Wrap(err, "member", errorx.KindInvalidArgument))
		return
	}
	if err := s.channels.AddMember(r.Context(), c, org, id, member); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LeaveChannel implements apigen.ServerInterface.
func (s *Server) LeaveChannel(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ChannelPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.channels.Leave(r.Context(), c, org, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMessages implements apigen.ServerInterface.
func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ChannelPath, params apigen.ListMessagesParams) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	before := ""
	if params.Before != nil {
		before = *params.Before
	}
	ms, err := s.channels.Messages(r.Context(), c, org, id, before)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messagesToAPI(ms))
}

// PostMessage implements apigen.ServerInterface.
func (s *Server) PostMessage(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ChannelPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.MessagePost
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	in := channel.PostInput{Body: body.Body}
	if body.Thread != nil {
		in.Thread = *body.Thread
	}
	if body.Mentions != nil {
		if in.Mentions, err = parseGRNs(*body.Mentions); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if body.Pages != nil {
		in.Pages = *body.Pages
	}
	m, err := s.channels.Post(r.Context(), c, org, id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageToAPI(m))
}

// SearchMessages implements apigen.ServerInterface.
func (s *Server) SearchMessages(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, params apigen.SearchMessagesParams) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ms, err := s.channels.Search(r.Context(), c, org, params.Q)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messagesToAPI(ms))
}

// GetThread implements apigen.ServerInterface.
func (s *Server) GetThread(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.MessagePath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ms, err := s.channels.Thread(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messagesToAPI(ms))
}

// ListPageMessages implements apigen.ServerInterface.
func (s *Server) ListPageMessages(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.PagePath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ms, err := s.channels.Linking(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messagesToAPI(ms))
}
