package cluster

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/garamsh/garam-bot/api/declaration/v1alpha1/manifest"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// The bounds on what a declaration carries (ADR-0187).
const (
	// DeclarationIDMaxLength is cluster-connection's bound on a
	// change's id; migration 000085 holds the same one.
	DeclarationIDMaxLength = 128
	// DeclaredObjectMaxBytes bounds a declared object once normalized.
	DeclaredObjectMaxBytes = 64 << 10
	// DeclarationPageSize is how many changes one page answers
	// (DECL-5); Config.DeclarationPageSize replaces it.
	DeclarationPageSize = 100
	// ChangeRetention is how long a change stays in a record's log
	// (DECL-6): a cursor before the oldest kept is answered 410.
	ChangeRetention = 30 * 24 * time.Hour
)

// The two operations a change carries (cluster-connection's Change).
const (
	OpApply  = "apply"
	OpDelete = "delete"
)

var (
	// ErrDeclarationNotFound answers an identifier no live declaration
	// of the record holds.
	ErrDeclarationNotFound = errorx.New(errorx.KindNotFound, "declaration not found")
	// ErrDeclaredObjectInvalid answers an object the connector would
	// hold as invalid (DECL-12), which the browser surface answers 422.
	ErrDeclaredObjectInvalid = errorx.New(errorx.KindInvalidArgument, "the object is not one the connector admits")
	// ErrDeclarationTaken answers an identifier already placed on
	// another record, which it names for good.
	ErrDeclarationTaken = errorx.New(errorx.KindAlreadyExists, "the identifier names a declaration placed on another record")
	// ErrDeclarationTombstoned answers an identifier whose declaration
	// was tombstoned: it is never placed again.
	ErrDeclarationTombstoned = errorx.New(errorx.KindAlreadyExists, "the identifier names a tombstoned declaration")
	// ErrDeclaredObjectTaken answers an object another live declaration
	// of the record already names.
	ErrDeclaredObjectTaken = errorx.New(errorx.KindAlreadyExists, "another live declaration of this cluster names the object")
	// ErrDeclaredObjectHeld answers an object the record's latest
	// report shows held by a cluster-owned object. It is a courtesy:
	// the report may be stale, and the connector's conflict is what
	// holds (gbo ADR 0089 decision 4).
	ErrDeclaredObjectHeld = errorx.New(errorx.KindAlreadyExists, "the cluster's latest report shows a cluster-owned object under this kind, namespace and name")
	// ErrCursorInvalid answers an after value that is no cursor of the
	// record.
	ErrCursorInvalid = errorx.New(errorx.KindInvalidArgument, "after is not a cursor of this cluster")
	// ErrCursorExpired answers a cursor at or before the position the
	// record's log was purged through (DECL-6), which the connection
	// answers 410 cursor_expired.
	ErrCursorExpired = errorx.New(errorx.KindFailedPrecondition, "the cursor is at or before the purged position")
)

// Declaration is one declaration placed on a record. A tombstoned one
// carries no object.
type Declaration struct {
	ID         string
	Cluster    grn.GRN
	Generation int64
	Object     DeclaredObject
	Tombstoned bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DeclaredObject is an object DECL-7 to DECL-9 admit, normalized to
// DECL-7's members. Only ParseDeclaredObject makes a non-zero one.
type DeclaredObject struct {
	kind, namespace, name string
	raw                   json.RawMessage
}

// Kind, Namespace and Name are the members two live declarations of a
// record never share.
func (o DeclaredObject) Kind() string      { return o.kind }
func (o DeclaredObject) Namespace() string { return o.namespace }
func (o DeclaredObject) Name() string      { return o.name }

// JSON is the normalized object, nil for the zero value.
func (o DeclaredObject) JSON() json.RawMessage { return o.raw }

// declaredObject is cluster-connection's DeclaredObject: what DECL-7
// lets a declaration carry, and no more.
type declaredObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Namespace   string            `json:"namespace"`
		Name        string            `json:"name"`
		Labels      map[string]string `json:"labels,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
	} `json:"metadata"`
	Spec map[string]any `json:"spec"`
}

var declarationID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ParseDeclaredObject admits raw where interface 1's own parser
// admits it, as the connector parses it (DECL-12): a declared kind
// (DECL-7), each secret by reference alone (DECL-8) and no image, since
// no field names one (DECL-9). It also refuses an object with no
// namespace, which the connector refuses, and answers the object with
// only DECL-7's members.
func ParseDeclaredObject(raw []byte) (DeclaredObject, error) {
	if _, err := manifest.Parse(raw); err != nil {
		// The parser's message names the field it refused, and quotes
		// only what this caller sent.
		return DeclaredObject{}, errorx.Wrap(ErrDeclaredObjectInvalid, err.Error())
	}
	var parsed declaredObject
	// UseNumber keeps every number of spec as it was written.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return DeclaredObject{}, errorx.Wrap(err, "decode an object interface 1 admits", errorx.KindInternal)
	}
	if parsed.Metadata.Namespace == "" {
		return DeclaredObject{}, errorx.Wrap(ErrDeclaredObjectInvalid, "DECL-7: metadata.namespace is missing")
	}
	normalized, err := json.Marshal(parsed)
	if err != nil {
		return DeclaredObject{}, errorx.Wrap(err, "encode a declared object", errorx.KindInternal)
	}
	if len(normalized) > DeclaredObjectMaxBytes {
		return DeclaredObject{}, errorx.New(errorx.KindInvalidArgument,
			"the object is over "+strconv.Itoa(DeclaredObjectMaxBytes)+" bytes")
	}
	return DeclaredObject{kind: parsed.Kind, namespace: parsed.Metadata.Namespace,
		name: parsed.Metadata.Name, raw: normalized}, nil
}

// CheckDeclarationID refuses an identifier cluster-connection's Change
// could not carry, or that garam does not mint the alphabet of.
func CheckDeclarationID(id string) error {
	if len(id) == 0 || len(id) > DeclarationIDMaxLength || !declarationID.MatchString(id) {
		return errorx.New(errorx.KindInvalidArgument,
			"a declaration's identifier is 1 to 128 letters, digits, '.', '_' or '-'")
	}
	return nil
}

// NewDeclarationID draws the identifier of a declaration whose writer
// named none.
func NewDeclarationID() (string, error) {
	b := make([]byte, identifierBytes)
	if _, err := rand.Read(b); err != nil {
		return "", errorx.Wrap(err, "cluster: draw a declaration identifier", errorx.KindInternal)
	}
	return "decl-" + hex.EncodeToString(b), nil
}

// Change is one entry of a record's log, or one apply of a full set.
type Change struct {
	Op         string
	ID         string
	Generation int64
	// Object is the normalized object of an apply, nil on a delete.
	Object json.RawMessage
}

// Page is cluster-connection's DeclarationPage.
type Page struct {
	Cursor  string
	Full    bool
	More    bool
	Changes []Change
}

// Position is where a record's log stands: the last position written,
// and the last one no longer kept.
type Position struct {
	Head          int64
	PurgedThrough int64
}

// Cursor is a parsed position in a record's log. A full-set cursor
// also names the position its full set began at, which its last page
// answers as the cursor to go on from. c.<n> reads the log after n;
// f.<base>.<sent> reads the live declarations last written after sent.
type Cursor struct {
	Full     bool
	Base     int64
	LastSent int64
}

func (c Cursor) String() string {
	if c.Full {
		return fmt.Sprintf("f.%d.%d", c.Base, c.LastSent)
	}
	return fmt.Sprintf("c.%d", c.LastSent)
}

// ParseCursor reads an after value against the record's position. An
// absent one starts a full set at the head (DECL-1).
func ParseCursor(after *string, at Position) (Cursor, error) {
	if after == nil {
		return Cursor{Full: true, Base: at.Head, LastSent: -1}, nil
	}
	parts := strings.Split(*after, ".")
	numbers := make([]int64, 0, 2)
	for _, p := range parts[1:] {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < -1 || n > at.Head {
			return Cursor{}, ErrCursorInvalid
		}
		numbers = append(numbers, n)
	}
	var parsed Cursor
	switch {
	case parts[0] == "c" && len(numbers) == 1:
		parsed = Cursor{Base: numbers[0], LastSent: numbers[0]}
	case parts[0] == "f" && len(numbers) == 2:
		parsed = Cursor{Full: true, Base: numbers[0], LastSent: numbers[1]}
	default:
		return Cursor{}, ErrCursorInvalid
	}
	if parsed.Base <= at.PurgedThrough {
		return Cursor{}, ErrCursorExpired
	}
	return parsed, nil
}

// Entry is one change as a record's log keeps it, or one live
// declaration as a full set sends it, with the position it was last
// written at.
type Entry struct {
	Seq    int64
	Change Change
}

// LogPage answers the page after a log cursor from entries, the log's
// entries after at.LastSent in order, at most size+1 of them (DECL-2,
// DECL-5).
func LogPage(at Cursor, entries []Entry, size int) Page {
	page := Page{Cursor: at.String(), Changes: make([]Change, 0, min(len(entries), size))}
	for _, e := range entries {
		if len(page.Changes) == size {
			page.More = true
			return page
		}
		page.Changes = append(page.Changes, e.Change)
		page.Cursor = Cursor{Base: e.Seq, LastSent: e.Seq}.String()
	}
	return page
}

// FullPage answers the next page of a full set from live, the record's
// live declarations last written after at.LastSent in that order, at
// most size+1 of them (DECL-1, DECL-5). Its last page answers the log
// cursor at the position the full set began at.
func FullPage(at Cursor, live []Entry, size int) Page {
	page := Page{Full: true, Changes: make([]Change, 0, min(len(live), size))}
	for _, e := range live {
		if len(page.Changes) == size {
			page.More = true
			page.Cursor = at.String()
			return page
		}
		page.Changes = append(page.Changes, e.Change)
		at.LastSent = e.Seq
	}
	page.Cursor = Cursor{Base: at.Base, LastSent: at.Base}.String()
	return page
}
