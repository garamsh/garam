package pgxtype

import "strings"

// likeEscaper escapes the characters LIKE and ILIKE read as other than
// themselves under their default escape character, the backslash.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Contains returns the LIKE pattern matching any text that holds s, in
// which every character of s matches only itself.
func Contains(s string) string {
	return "%" + likeEscaper.Replace(s) + "%"
}
