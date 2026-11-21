package policyrelay

import "time"

// AuditKind is what an audit record is (ADR-0148).
type AuditKind string

const (
	// AuditWrite is one write of a policy.
	AuditWrite AuditKind = "write"
	// AuditDenial is the denials of one request shape, collapsed.
	AuditDenial AuditKind = "denial"
	// AuditDenialOverflow is the denials of a UTC day that no longer
	// fit a record of their own.
	AuditDenialOverflow AuditKind = "denial-overflow"
)

// AuditRecord is one record of an organization's audit. It holds no
// policy text, no context, no token and no fault text: what the
// service keeps of a write or a denial, and nothing more.
type AuditRecord struct {
	ID   int64
	Kind AuditKind
	// FirstAt is when a write was made or a denial first occurred;
	// LastAt is when a denial last occurred, and a write's FirstAt.
	FirstAt time.Time
	LastAt  time.Time
	// Count is how many identical denials the record holds; 1 for a
	// write.
	Count int64
	// Service is the service that made the request, empty on an
	// overflow record.
	Service string
	// Subject is the GRN of who asked or wrote, empty for a write the
	// service made with no writer and on an overflow record.
	Subject  string
	Action   string
	Resource string
	// Outcome is deny, created, version-added, activated, rolled-back
	// or removed.
	Outcome string
	// PolicyIDs are the policies of a denial's reasons, or a write's
	// policy.
	PolicyIDs []string
	// VersionFrom and VersionTo are the default a write moved from and
	// the version it wrote or moved to, 0 where there is none.
	VersionFrom int32
	VersionTo   int32
	// Day is the UTC day an overflow record holds, zero otherwise.
	Day time.Time
}

// AuditPage is one page of records, newest first. NextPageToken reads
// the next, and is empty at the end.
type AuditPage struct {
	Records       []AuditRecord
	NextPageToken string
}
