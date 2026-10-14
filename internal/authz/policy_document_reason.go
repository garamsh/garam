package authz

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// The reasons a refused policy document carries (api/openapi.yaml
// `Error.reason`), one per rule [ParsePolicyDocument] holds. Each comes
// with the JSON Pointer of the place it is about, empty where that is
// the document as a whole.
const (
	ReasonPolicyDocumentMalformed       = "policy_document_malformed"
	ReasonPolicyDocumentTrailingData    = "policy_document_trailing_data"
	ReasonPolicyDocumentWrongType       = "policy_document_wrong_type"
	ReasonPolicyDocumentUnknownKey      = "policy_document_unknown_key"
	ReasonPolicyDocumentVersionMissing  = "policy_document_version_missing"
	ReasonPolicyDocumentStatementEmpty  = "policy_document_statement_empty"
	ReasonPolicyDocumentEffectInvalid   = "policy_document_effect_invalid"
	ReasonPolicyDocumentActionMissing   = "policy_document_action_missing"
	ReasonPolicyDocumentResourceMissing = "policy_document_resource_missing"
)

// documentRefusal is a refused policy document, as a client reads it.
func documentRefusal(code, path, message string) error {
	return errorx.NewWithReason(errorx.KindInvalidArgument,
		errorx.Reason{Code: code, Path: path}, "policy document: "+message)
}

// shape is what the decoder accepts at one place in a document, so a
// refusal it gave can be located: the decoder says that a key is
// unknown or a value has the wrong type, and not where.
type shape struct {
	kind   shapeKind
	keys   map[string]shape
	open   bool // an object whose keys nothing names, as Condition is
	member *shape
}

type shapeKind int

const (
	shapeAny shapeKind = iota
	shapeString
	shapeObject
	shapeArray
)

var stringList = shape{kind: shapeArray, member: &shape{kind: shapeString}}

var statementShape = shape{kind: shapeObject, keys: map[string]shape{
	"Sid":       {kind: shapeString},
	"Effect":    {kind: shapeString},
	"Action":    stringList,
	"Resource":  stringList,
	"Condition": {kind: shapeAny},
}}

var documentShape = shape{kind: shapeObject, keys: map[string]shape{
	"Version":   {kind: shapeString},
	"Statement": {kind: shapeArray, member: &statementShape},
}}

// locateDecodeRefusal names the rule a document the decoder refused
// breaks, and where. It decides nothing: the decoder already refused,
// and a refusal it cannot place reads as malformed at the root.
func locateDecodeRefusal(raw []byte, cause error) error {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err == nil {
		if code, path, message, found := documentShape.find(value, ""); found {
			return errorx.WrapWithReason(cause, "policy document: "+message,
				errorx.KindInvalidArgument, errorx.Reason{Code: code, Path: path})
		}
	}
	return errorx.WrapWithReason(cause, "policy document: not a JSON object in the policy format",
		errorx.KindInvalidArgument, errorx.Reason{Code: ReasonPolicyDocumentMalformed})
}

// find walks value against s and answers the first place it does not
// fit. Keys match as the decoder matches them, without regard to case,
// and null fits every shape, as the decoder takes it.
func (s shape) find(value any, path string) (code, at, message string, found bool) {
	if value == nil || s.kind == shapeAny {
		return "", "", "", false
	}
	switch s.kind {
	case shapeString:
		if _, ok := value.(string); !ok {
			return ReasonPolicyDocumentWrongType, path, pointerName(path) + " must be a string", true
		}
	case shapeArray:
		list, ok := value.([]any)
		if !ok {
			return ReasonPolicyDocumentWrongType, path, pointerName(path) + " must be a list", true
		}
		for i, member := range list {
			if code, at, message, found := s.member.find(member, path+"/"+strconv.Itoa(i)); found {
				return code, at, message, true
			}
		}
	case shapeObject:
		object, ok := value.(map[string]any)
		if !ok {
			return ReasonPolicyDocumentWrongType, path, pointerName(path) + " must be an object", true
		}
		for _, key := range sortedKeys(object) {
			name, known := s.keyFor(key)
			at := path + "/" + escapePointer(key)
			if !known {
				return ReasonPolicyDocumentUnknownKey, at, "unknown key " + strconv.Quote(key), true
			}
			if code, at, message, found := s.keys[name].find(object[key], at); found {
				return code, at, message, true
			}
		}
	}
	return "", "", "", false
}

// keyFor answers the model's name for key, matched without regard to
// case as encoding/json matches a struct field.
func (s shape) keyFor(key string) (string, bool) {
	for name := range s.keys {
		if strings.EqualFold(name, key) {
			return name, true
		}
	}
	return "", false
}

// sortedKeys orders an object's keys so the first refusal reported is
// the same on every run.
func sortedKeys(object map[string]any) []string {
	keys := slices.Collect(maps.Keys(object))
	slices.Sort(keys)
	return keys
}

// escapePointer escapes one JSON Pointer reference token (RFC 6901 §3).
func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}

// pointerName is how a message names the place a pointer leads to.
func pointerName(path string) string {
	if path == "" {
		return "the document"
	}
	return path
}
