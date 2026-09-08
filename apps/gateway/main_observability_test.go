//go:build !js

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTelemetryInitializationPrecedesDependenciesAndListeners(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]token.Pos{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		key := identifier.Name + "." + selector.Sel.Name
		if key == "telemetry.New" || key == "logstore.New" || key == "apiServer.Start" {
			if positions[key] == token.NoPos {
				positions[key] = call.Pos()
			}
		}
		return true
	})
	for _, key := range []string{"telemetry.New", "logstore.New", "apiServer.Start"} {
		if positions[key] == token.NoPos {
			t.Fatalf("did not find lifecycle call %s", key)
		}
	}
	if !(positions["telemetry.New"] < positions["logstore.New"] && positions["telemetry.New"] < positions["apiServer.Start"]) {
		t.Fatalf("telemetry must initialize before dependencies/listeners: %#v", positions)
	}
}
