package delivery

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// maxEntryBytes bounds one outbox entry. garam admits a body of 65536
// characters, four bytes each at most, and JSON may escape one into
// six; the runtime's own ingress stops at the same mebibyte.
const maxEntryBytes = 1 << 20

// entrySuffix is the name the runtime's writer gives a finished entry.
// Its temporary files are dot-prefixed and end otherwise, so neither is
// ever read as an entry.
const entrySuffix = ".json"

// Outbox is the runtime's durable outbox directory, read at the file
// boundary: a tool the runtime spawns can write there, so membership in
// the directory proves nothing about a file and every entry is opened
// only as a bounded, singly linked regular file inside it, never
// through a link (ADR-0084).
type Outbox struct {
	dir  string
	root *os.Root
}

// NewOutbox names the outbox directory. It need not exist yet: the
// runtime makes it on its first reply.
func NewOutbox(dir string) (*Outbox, error) {
	if dir == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: outbox directory is required")
	}
	return &Outbox{dir: dir}, nil
}

// Entry is one reply the runtime recorded, as it recorded it. Its
// origin stays as written until garam acknowledges it: nothing here is
// rewritten, re-keyed or re-stamped.
type Entry struct {
	Name       string    `json:"-"`
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	Agent      string    `json:"agent_id"`
	Epoch      string    `json:"assignment_epoch"`
	Generation string    `json:"generation"`
	InReplyTo  []string  `json:"in_reply_to"`
	Channel    string    `json:"channel"`
	Target     string    `json:"target"`
	Content    string    `json:"content"`

	raw  []byte
	file os.FileInfo
}

// Names answers the entries the outbox holds, in name order, which is
// the order the runtime's writer created them in.
func (o *Outbox) Names() ([]string, error) {
	root, err := o.opened()
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: outbox", errorx.KindUnavailable)
	}
	defer func() { _ = dir.Close() }()
	all, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: outbox", errorx.KindUnavailable)
	}
	names := all[:0]
	for _, name := range all {
		if strings.HasSuffix(name, entrySuffix) && !strings.HasPrefix(name, ".") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// Identity answers what the named file is without opening it, so an
// entry already set aside is not read again until it is replaced.
func (o *Outbox) Identity(name string) (os.FileInfo, error) {
	root, err := o.opened()
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: outbox entry", errorx.KindNotFound)
	}
	return info, nil
}

// Read opens one entry at the file boundary and parses it. A link, a
// file of any other type, one with a second name, one larger than an
// entry can be, and one the name does not match are all refused with
// KindFailedPrecondition, and nothing of their contents is answered. A
// file the system could not open or read is KindUnavailable.
func (o *Outbox) Read(name string) (*Entry, error) {
	raw, info, err := o.open(name)
	if err != nil {
		return nil, err
	}
	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry is not one")
	}
	if entry.ID == "" || entry.ID+entrySuffix != name {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry is not named for its identifier")
	}
	entry.Name, entry.raw, entry.file = name, raw, info
	return &entry, nil
}

// Remove deletes an entry garam acknowledged, and only while the file
// under its name is still the one that was read, byte for byte: a
// replacement is another reply, and is kept.
func (o *Outbox) Remove(e *Entry) error {
	raw, info, err := o.open(e.Name)
	if err != nil {
		return err
	}
	if !os.SameFile(info, e.file) || !bytes.Equal(raw, e.raw) {
		return errorx.New(errorx.KindAborted, "delivery: outbox entry was replaced after it was read")
	}
	root, err := o.opened()
	if err != nil {
		return err
	}
	if err := root.Remove(e.Name); err != nil {
		return errorx.Wrap(err, "delivery: outbox entry", errorx.KindUnavailable)
	}
	return nil
}

// open reads the named file only if the name is a regular file with one
// link, the descriptor opened is that same file, and it fits the bound.
// The root refuses a link that leaves the directory; the name's own
// type and the descriptor's identity refuse one that stays inside it.
func (o *Outbox) open(name string) ([]byte, os.FileInfo, error) {
	root, err := o.opened()
	if err != nil {
		return nil, nil, err
	}
	named, err := root.Lstat(name)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "delivery: outbox entry", errorx.KindNotFound)
	}
	if !named.Mode().IsRegular() {
		return nil, nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry is not a regular file")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		// The name was a regular file a moment ago, so a failed open is
		// the system's (no descriptor, no permission, an I/O error) or a
		// replacement the next read refuses: neither is this entry's.
		return nil, nil, errorx.Wrap(err, "delivery: outbox entry", errorx.KindUnavailable)
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return nil, nil, errorx.Wrap(err, "delivery: outbox entry", errorx.KindUnavailable)
	}
	if !os.SameFile(named, opened) || !opened.Mode().IsRegular() {
		return nil, nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry changed while it was opened")
	}
	if stat, ok := opened.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry has another name")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxEntryBytes+1))
	if err != nil {
		return nil, nil, errorx.Wrap(err, "delivery: outbox entry", errorx.KindUnavailable)
	}
	if len(raw) > maxEntryBytes {
		return nil, nil, errorx.New(errorx.KindFailedPrecondition, "delivery: outbox entry is larger than an entry can be")
	}
	return raw, opened, nil
}

// opened is the directory as a root, opened on first use because the
// runtime makes it on its first reply.
func (o *Outbox) opened() (*os.Root, error) {
	if o.root != nil {
		return o.root, nil
	}
	root, err := os.OpenRoot(o.dir)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: outbox", errorx.KindUnavailable)
	}
	o.root = root
	return root, nil
}
