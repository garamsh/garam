package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// reasonPackages are the packages whose refusals carry a reason.
var reasonPackages = []string{"../org", "../authz"}

// reasonConstants reads every `Reason*` string constant the reason
// packages declare, from their source, so a code added there is in this
// test without anyone listing it.
func reasonConstants(t *testing.T) []string {
	t.Helper()
	var codes []string
	for _, dir := range reasonPackages {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			require.NoError(t, err)
			for _, decl := range parsed.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						if !strings.HasPrefix(name.Name, "Reason") || i >= len(value.Values) {
							continue
						}
						lit, ok := value.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						code, err := strconv.Unquote(lit.Value)
						require.NoError(t, err)
						codes = append(codes, code)
					}
				}
			}
		}
	}
	slices.Sort(codes)
	return codes
}

// documentedReasons reads the codes api/openapi.yaml lists under
// `Error.reason`: every backticked snake_case word in its description.
func documentedReasons(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Description string `yaml:"description"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	description := spec.Components.Schemas["Error"].Properties["reason"].Description
	require.NotEmpty(t, description, "Error.reason is described")
	var codes []string
	for _, match := range regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`").FindAllStringSubmatch(description, -1) {
		codes = append(codes, match[1])
	}
	slices.Sort(codes)
	return codes
}

// TestReasonCodes_AreTheOnesTheSpecificationLists holds the reason
// constants the server raises and the codes api/openapi.yaml documents
// to one set, in both directions: a constant added without its line in
// the specification fails, and so does a documented code nothing
// raises. Each side is read non-empty first, so an empty pair cannot
// pass as agreeing.
func TestReasonCodes_AreTheOnesTheSpecificationLists(t *testing.T) {
	constants := reasonConstants(t)
	documented := documentedReasons(t)
	require.NotEmpty(t, constants)
	require.NotEmpty(t, documented)

	assert.Equal(t, constants, documented)
}
