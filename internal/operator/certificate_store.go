package operator

import "context"

// CertificateStore persists which certificate garam last issued for an
// operator. It is separate from Store because the two answer different
// questions: what the organization has registered, and what the holder
// of a certificate may still renew. It has no read — the write below
// answers whether the presented certificate is still the current one
// (ADR-0046).
type CertificateStore interface {
	// Adopt records current as the operator's certificate, whatever the
	// operator held before. This is the out-of-band mint (ADR-0032),
	// which is also the recovery: it takes the lineage from every
	// certificate that already names the operator.
	Adopt(ctx context.Context, current Current) error
	// Advance records current in place of predecessor, or ErrSuperseded
	// when the operator's certificate is not predecessor. An operator
	// with no record yet is adopted by its first renewal: nothing has
	// been superseded where nothing was recorded.
	Advance(ctx context.Context, current Current, predecessor Serial) error
}
