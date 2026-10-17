package message

// Holding is where a signing key's private half is held (ADR-0104):
// sealed into the key's own row, or kept by a Transit key that signs
// for it. It is one of [SealedKey] and [TransitVersion] and nothing
// else, so a row holds exactly one.
type Holding interface {
	holding()
}

// SealedKey is a private half sealed under the key-encryption key and
// bound to its own row (ADR-0101).
type SealedKey []byte

// TransitVersion names the version of an OpenBao Transit key that
// signs for a key whose private half never leaves Transit (ADR-0104).
type TransitVersion struct {
	Mount   string
	Key     string
	Version int
}

func (SealedKey) holding()      {}
func (TransitVersion) holding() {}
