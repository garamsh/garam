package agent

import (
	"encoding/json"
	"slices"
)

// Inventory record kinds: an agent garam holds a row for, a definition
// no operator claimed, and a template.
const (
	InventoryAgent      = "agent"
	InventoryDefinition = "definition"
	InventoryTemplate   = "template"
)

// Dispositions an inventory record states for its source.
const (
	// DispositionImport is a source the Operator imports by the GRN.
	DispositionImport = "import"
	// DispositionPreserveAndBlock is a source garam keeps as stored and
	// whose cutover it refuses until it parses.
	DispositionPreserveAndBlock = "preserve-and-block"
	// DispositionRetain is a record that stays in garam and is not
	// imported: an unclaimed definition, a template, or the source of
	// an agent already fenced.
	DispositionRetain = "retain"
)

// InventoryRecord is one line of a cutover inventory: one source
// record, what holds it now and what cutover does with it. It carries
// no value, only a SHA-256 of each, so printing it discloses no
// setting.
type InventoryRecord struct {
	Record string `json:"record"`
	GRN    string `json:"grn"`
	Name   string `json:"name,omitempty"`
	// OperatorGRN is the operator the source digest covers: the
	// definition's, or the assignee of an agent with no definition.
	OperatorGRN string `json:"operatorGrn,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Epoch       int    `json:"epoch,omitempty"`
	Mode        Mode   `json:"mode,omitempty"`
	// Lineage is the tracked credential lineage, empty for the legacy
	// cohort, whose certificates garam never recorded.
	Lineage            string `json:"lineage,omitempty"`
	TrackedCredentials int64  `json:"trackedCredentials"`
	// HasDefinition is false for an agent with no definition, which
	// cuts over with empty values.
	HasDefinition bool              `json:"hasDefinition"`
	Keys          []string          `json:"keys"`
	ValueSHA256   map[string]string `json:"valueSha256"`
	SourceDigest  string            `json:"sourceDigest,omitempty"`
	Class         CutoverClass      `json:"class,omitempty"`
	Disposition   string            `json:"disposition"`
	Attempt       *InventoryAttempt `json:"attempt,omitempty"`
}

// InventoryAttempt is an agent's most recent cutover attempt.
type InventoryAttempt struct {
	ImportID ImportID     `json:"importId"`
	Stage    CutoverStage `json:"stage"`
}

// InventorySummary closes an inventory with how many of each source
// record it accounted for, so a reader can check the lines against the
// organization's own counts.
type InventorySummary struct {
	Record      string `json:"record"`
	Org         string `json:"org"`
	Agents      int    `json:"agents"`
	Definitions int    `json:"definitions"`
	Templates   int    `json:"templates"`
}

// StoredValues is a values payload as stored, read without trusting
// it: the keys it holds and a digest of each value, and the parsed
// values where it parses.
type StoredValues struct {
	Keys        []string
	ValueSHA256 map[string]string
	// Values is nil where the payload does not parse.
	Values Values
}

// ReadStoredValues reads a stored payload. One that is not an object
// of strings, or that ParseValues refuses, is reported unparsed rather
// than failing the read: one corrupt row is a record, not an outage. A
// value that is not a string is digested over its JSON text.
func ReadStoredValues(stored []byte) StoredValues {
	out := StoredValues{Keys: []string{}, ValueSHA256: map[string]string{}}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stored, &raw); err != nil {
		return out
	}
	strs := make(map[string]string, len(raw))
	for k, v := range raw {
		out.Keys = append(out.Keys, k)
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			out.ValueSHA256[k] = ValueDigest(string(v)).String()
			strs = nil
			continue
		}
		out.ValueSHA256[k] = ValueDigest(s).String()
		if strs != nil {
			strs[k] = s
		}
	}
	slices.Sort(out.Keys)
	if strs != nil {
		if values, err := ParseValues(strs); err == nil {
			out.Values = values
		}
	}
	return out
}
