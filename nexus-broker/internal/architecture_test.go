package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rule defines an architectural constraint
type Rule struct {
	Package     string
	ShouldNot   []string // Substrings of import paths that are forbidden
	Description string
}

func TestSeparationOfConcerns(t *testing.T) {
	modulePrefix := "github.com/Prescott-Data/nexus-framework/nexus-broker"

	rules := []Rule{
		{
			Package: "internal/domain",
			ShouldNot: []string{
				"net/http",
				"github.com/jmoiron/sqlx",
				"github.com/lib/pq",
				modulePrefix + "/pkg/handlers",
				modulePrefix + "/internal/service",
				modulePrefix + "/internal/repository",
			},
			Description: "Domain models must be pure and ignorant of HTTP, database drivers, and other layers.",
		},
		{
			Package: "internal/repository",
			ShouldNot: []string{
				"net/http",
				modulePrefix + "/pkg/handlers",
				modulePrefix + "/internal/service",
			},
			Description: "Repositories must not depend on HTTP handlers or business logic services.",
		},
		{
			Package: "internal/service",
			ShouldNot: []string{
				modulePrefix + "/pkg/handlers",
				"github.com/jmoiron/sqlx",
			},
			Description: "Services must not depend on HTTP handlers or raw SQL drivers (use repositories instead).",
		},
		{
			Package: "pkg/handlers",
			ShouldNot: []string{
				modulePrefix + "/internal/repository",
			},
			Description: "HTTP Handlers must not bypass the Service layer to talk directly to Repositories.",
		},
		{
			Package: "pkg/provider",
			ShouldNot: []string{
				modulePrefix + "/pkg/handlers",
				modulePrefix + "/internal/service",
			},
			Description: "The Provider store infrastructure must not depend on HTTP handlers or business logic.",
		},
		{
			Package: "pkg/telemetry",
			ShouldNot: []string{
				modulePrefix + "/pkg/handlers",
				modulePrefix + "/internal/service",
			},
			Description: "Telemetry collectors must be independent of HTTP handlers and business logic.",
		},
	}

	basePath := ".." // We are inside internal, so .. is the broker root

	for _, rule := range rules {
		t.Run(rule.Package, func(t *testing.T) {
			targetDir := filepath.Join(basePath, rule.Package)
			
			// Walk the target directory
			err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				
				// Only analyze Go files, skip tests
				if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}

				fset := token.NewFileSet()
				node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatalf("failed to parse %s: %v", path, err)
				}

				for _, imp := range node.Imports {
					importPath := strings.Trim(imp.Path.Value, `"`)
					
					for _, forbidden := range rule.ShouldNot {
						if strings.Contains(importPath, forbidden) {
							t.Errorf("\nViolation in %s\nRule: %s\nForbidden Import Found: %s", 
								path, rule.Description, importPath)
						}
					}
				}
				return nil
			})

			if err != nil {
				// If the directory doesn't exist yet, we just skip (e.g. if we are running from a different root)
				if os.IsNotExist(err) {
					t.Logf("Directory %s does not exist, skipping.", targetDir)
				} else {
					t.Fatalf("error walking directory %s: %v", targetDir, err)
				}
			}
		})
	}
}
