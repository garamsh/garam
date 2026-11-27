package bot

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Declare places raw on g under id, or under an identifier it draws
// where id is empty, answering the declaration and whether it was new
// (ADR-0187). raw is refused unless ParseDeclaredObject admits it, and
// nothing refused reaches declarations.
func Declare(ctx context.Context, declarations DeclarationStore, g grn.GRN, id string, raw []byte) (Declaration, bool, error) {
	if id == "" {
		drawn, err := NewDeclarationID()
		if err != nil {
			return Declaration{}, false, err
		}
		id = drawn
	}
	if err := CheckDeclarationID(id); err != nil {
		return Declaration{}, false, err
	}
	object, err := ParseDeclaredObject(raw)
	if err != nil {
		return Declaration{}, false, err
	}
	return declarations.Declare(ctx, g, id, object)
}
