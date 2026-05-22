package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoPlanServerClientV1URLs is a structural guard against reintroducing
// `/api/v1/...` URLs in plan-server client code. It complements the runtime
// probes in plans_client_test.go (TestGetHealth_UsesFilPrefix,
// TestPlanServerClientUsesFilPrefix), which only catch regressions in methods
// they explicitly call — a newly-added method with a hardcoded `/api/v1` URL
// would slip past them. This walks every string literal in the plan-server
// client surface, so the next miss fails this test even without a dedicated
// runtime probe.
//
// Out of scope: api/client.go (Spoolman URLs) and server/prusa.go (outbound
// Prusa REST) both legitimately use `/api/v1`.
func TestNoPlanServerClientV1URLs(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) returned !ok; cannot locate test source")
	}
	apiDir := filepath.Dir(thisFile)
	planDir := filepath.Join(apiDir, "..", "plan")

	files := []string{
		filepath.Join(apiDir, "plans_client.go"),
		filepath.Join(apiDir, "health.go"),
	}

	matches, err := filepath.Glob(filepath.Join(planDir, "remote_*.go"))
	if err != nil {
		t.Fatalf("glob plan/remote_*.go: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no plan/remote_*.go files matched; test path resolution is wrong")
	}
	for _, m := range matches {
		// Test files can legitimately reference `/api/v1` in mock paths;
		// only production sources are in scope.
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		files = append(files, m)
	}

	fset := token.NewFileSet()
	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.Contains(lit.Value, "/api/v1") {
				pos := fset.Position(lit.Pos())
				t.Errorf("%s:%d: string literal contains /api/v1: %s",
					filepath.Base(pos.Filename), pos.Line, lit.Value)
			}
			return true
		})
	}
}
