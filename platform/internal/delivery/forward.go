package delivery

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// replyChannel is the channel an outbox entry names when its target is
// a garam identity. An entry for any other channel is not this
// adapter's to carry.
const replyChannel = "garam"

// maxReplyBody is the most characters garam admits in a body. An entry
// over it is set aside rather than shortened.
const maxReplyBody = 65536

// forwarding is the forwarder's memory, held for this process alone: a
// restart offers every entry again, under the same identity.
type forwarding struct {
	// aside is every entry set aside, by name, with the file it was:
	// it is not offered again unless that file is replaced.
	aside map[string]os.FileInfo
	// later is when an entry is offered again, and how long it waited
	// last: one whose creation garam's clock has not yet reached, or
	// one garam answered outside the contract (ADR-0108).
	later map[string]retry
	// backoff is how long the whole outbox waits after garam did not
	// answer, doubling up to its cap.
	backoff time.Duration
}

type retry struct {
	at   time.Time
	wait time.Duration
}

// forward offers every reply the runtime recorded to garam while
// protected calls are let through, and removes each only on an
// acknowledgement of exactly it (ADR-0079, ADR-0084). Entries an
// earlier generation left are offered the same way, under what this
// process presents and with their origin unchanged.
func (s *service) forward(ctx context.Context) {
	f := forwarding{aside: map[string]os.FileInfo{}, later: map[string]retry{}}
	for ctx.Err() == nil {
		if err := s.ends.Execution.Await(ctx); err != nil {
			return
		}
		delay := s.cfg.OutboxInterval
		if !s.pass(ctx, &f) {
			f.backoff = s.backoff(f.backoff)
			delay = f.backoff
		} else {
			f.backoff = 0
		}
		s.wait(ctx, delay)
	}
}

// entriesPerPass bounds how many entries one pass offers; the next pass
// takes the next ones.
const entriesPerPass = 256

// pass offers the oldest entries not set aside or put off, once each,
// in the order they were written, and answers false when garam did not
// answer, which ends the pass. It first forgets what it held for files
// that are gone, so its memory is bounded by what the outbox holds.
func (s *service) pass(ctx context.Context, f *forwarding) bool {
	s.forget(f)
	names, err := s.ends.Outbox.Names(entriesPerPass, func(name string) bool { return s.passedOver(f, name) })
	if err != nil {
		slog.DebugContext(ctx, "outbox not read", slog.String("error", err.Error()))
		return true
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return true
		}
		if !s.offer(ctx, f, name) {
			return false
		}
	}
	return true
}

// passedOver reports whether a pass skips an entry: one set aside whose
// file is unchanged, or one put off until later.
func (s *service) passedOver(f *forwarding, name string) bool {
	if file, set := f.aside[name]; set {
		if now, err := s.ends.Outbox.Identity(name); err == nil && os.SameFile(now, file) && now.ModTime().Equal(file.ModTime()) {
			return true
		}
		delete(f.aside, name)
	}
	r, waiting := f.later[name]
	return waiting && time.Now().Before(r.at)
}

// forget drops what the forwarder holds about files no longer there.
func (s *service) forget(f *forwarding) {
	for name := range f.aside {
		if _, err := s.ends.Outbox.Identity(name); errorx.IsKind(err, errorx.KindNotFound) {
			delete(f.aside, name)
		}
	}
	for name := range f.later {
		if _, err := s.ends.Outbox.Identity(name); errorx.IsKind(err, errorx.KindNotFound) {
			delete(f.later, name)
		}
	}
}

// offer reads one entry at the file boundary, offers it, and acts on
// garam's answer. It answers false when garam did not answer or the
// execution closed, which ends the pass.
func (s *service) offer(ctx context.Context, f *forwarding, name string) bool {
	entry, err := s.ends.Outbox.Read(name)
	switch {
	case errorx.IsKind(err, errorx.KindFailedPrecondition):
		s.setAside(ctx, f, name, nil, err.Error())
		return true
	case errorx.IsKind(err, errorx.KindUnavailable):
		// Not the entry's fault: it is read again after a backoff.
		s.later(f, name)
		slog.WarnContext(ctx, "outbox entry not read", slog.String("name", name), slog.String("error", err.Error()))
		return true
	case err != nil:
		return true
	}
	outbound, reason := s.outboundOf(entry)
	if reason != "" {
		s.setAside(ctx, f, name, entry, reason)
		return true
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	stored, err := s.ends.Queue.Post(callCtx, outbound)
	cancel()
	switch {
	case err == nil && stored.Acknowledges(s.ends.Agent, outbound):
		delete(f.later, name)
		s.acknowledged(ctx, entry)
		return true
	case err == nil:
		s.setAside(ctx, f, name, entry, "garam stored something other than this reply")
		return true
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		// The fence closed the execution; the loop waits on it.
		return false
	case errorx.IsKind(err, errorx.KindUnavailable):
		slog.WarnContext(ctx, "reply not forwarded; garam did not answer",
			slog.String("entry", entry.ID), slog.String("error", err.Error()))
		return false
	case errorx.IsKind(err, errorx.KindOutOfRange):
		s.later(f, name)
		slog.WarnContext(ctx, "reply not forwarded yet; garam's clock has not reached its creation",
			slog.String("entry", entry.ID))
		return true
	case errors.Is(err, ErrUnsupportedPeer):
		// The peer is not this entry's verdict: an upgrade may serve
		// the contract while this process runs (ADR-0108).
		s.later(f, name)
		slog.WarnContext(ctx, "reply not forwarded yet; garam does not serve its contract",
			slog.String("entry", entry.ID), slog.String("error", err.Error()))
		return true
	}
	s.setAside(ctx, f, name, entry, err.Error())
	return true
}

// later offers an entry again after its own backoff, doubling up to
// the cap each time it is put off.
func (s *service) later(f *forwarding, name string) {
	r := f.later[name]
	r.wait = s.backoff(r.wait)
	r.at = time.Now().Add(r.wait)
	f.later[name] = r
}

// outboundOf is what an entry asks garam to store, or the reason it is
// not this adapter's to offer. An entry of unknown age is never
// offered: nothing here can supply the instant it was created.
func (s *service) outboundOf(e *Entry) (Outbound, string) {
	switch {
	case e.Agent != s.ends.Agent.String():
		return Outbound{}, "the entry is another agent's"
	case e.Channel != replyChannel:
		return Outbound{}, "the entry names another channel"
	case e.CreatedAt.IsZero():
		return Outbound{}, "the entry has no creation time"
	case e.Content == "" || len([]rune(e.Content)) > maxReplyBody:
		return Outbound{}, "the entry's content is empty or larger than garam admits"
	}
	recipient, err := grn.Parse(e.Target)
	if err != nil {
		return Outbound{}, "the entry's target is not a GRN"
	}
	return Outbound{Recipient: recipient, IdempotencyKey: e.ID, Body: e.Content, CreatedAt: e.CreatedAt}, ""
}

// acknowledged removes an entry garam acknowledged, unless the file was
// replaced since it was read, which keeps the replacement.
func (s *service) acknowledged(ctx context.Context, e *Entry) {
	if err := s.ends.Outbox.Remove(e); err != nil {
		slog.WarnContext(ctx, "acknowledged reply not removed",
			slog.String("entry", e.ID), slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "reply forwarded",
		slog.String("entry", e.ID),
		slog.String("origin_generation", e.Generation),
		slog.String("origin_epoch", e.Epoch),
		slog.Any("in_reply_to", e.InReplyTo))
}

// setAside keeps an entry where it is and stops offering it until its
// file is replaced, logging why once. Nothing about an entry's content
// or a credential reaches the line.
func (s *service) setAside(ctx context.Context, f *forwarding, name string, e *Entry, reason string) {
	file, err := s.ends.Outbox.Identity(name)
	if err != nil {
		return
	}
	f.aside[name] = file
	delete(f.later, name)
	attrs := []any{slog.String("name", name), slog.String("reason", reason)}
	if e != nil {
		attrs = append(attrs,
			slog.String("entry", e.ID),
			slog.String("origin_generation", e.Generation),
			slog.String("origin_epoch", e.Epoch),
			slog.Any("in_reply_to", e.InReplyTo))
	}
	slog.ErrorContext(ctx, "reply kept in the outbox and not offered again", attrs...)
}
