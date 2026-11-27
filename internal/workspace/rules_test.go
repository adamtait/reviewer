// SPDX-License-Identifier: MIT

package workspace_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// serviceDirs are the trees that make up the service's own code. The index
// decoder and fetch helpers moved out from under internal/workspace so that the
// CLI could share them; they are still the service's code, and still held to
// its rules.
var serviceDirs = []string{
	".",
	filepath.Join("..", "..", "cmd", "workspace"),
	filepath.Join("..", "scip"),
	filepath.Join("..", "scipfetch"),
}

// forEachServiceFile parses the imports of every non-test Go file in the service.
func forEachServiceFile(t *testing.T, visit func(path string, imports []string)) {
	t.Helper()
	fset := token.NewFileSet()
	for _, dir := range serviceDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			var imports []string
			for _, spec := range file.Imports {
				name, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				imports = append(imports, name)
			}
			visit(path, imports)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// The service never starts a program itself. Goal 7 of the design is that it
// never executes pull request code, and the simplest way to hold that is for its
// own packages to have no way of executing anything: the one program it does
// start, the secrets scanner, is reached through the CLI's scanner package.
func TestNoServicePackageStartsAProgram(t *testing.T) {
	checked := 0
	forEachServiceFile(t, func(path string, imports []string) {
		checked++
		for _, name := range imports {
			if name == "os/exec" {
				t.Errorf("%s imports os/exec: the service's own packages must not start programs", path)
			}
		}
	})
	if checked == 0 {
		t.Fatal("no service source files were found, so the rule checked nothing")
	}
}

// forEachCall parses every non-test Go file of the service in full and reports
// each call of the form name.Function(...), with the import path that name
// stands for in that file, or "" when the name is not a package.
func forEachCall(t *testing.T, visit func(path, pkg, function string)) {
	t.Helper()
	fset := token.NewFileSet()
	for _, dir := range serviceDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			// The name each import is known by in this file.
			names := map[string]string{}
			for _, spec := range file.Imports {
				full, _ := strconv.Unquote(spec.Path.Value)
				name := full[strings.LastIndex(full, "/")+1:]
				if spec.Name != nil {
					name = spec.Name.Name
				}
				names[name] = full
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg := ""
				if ident, ok := sel.X.(*ast.Ident); ok {
					pkg = names[ident.Name]
				}
				visit(path, pkg, sel.Sel.Name)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

const (
	modelPkg    = "github.com/adamtait/reviewer/internal/model"
	gitleaksPkg = "github.com/adamtait/reviewer/internal/analyzers/gitleaks"
)

// in reports whether a file is in one of the service's packages, named by its
// directory under internal/workspace, or "cmd" for the binary.
func in(path, pkg string) bool {
	if pkg == "cmd" {
		return strings.Contains(filepath.ToSlash(path), "cmd/workspace/")
	}
	return strings.HasPrefix(filepath.ToSlash(path), pkg+"/")
}

// The secrets scanner is run one fixed way, by one package. A second caller
// would be a second way to run it, and content must not be able to choose.
func TestOnlyTheScanPackageRunsTheScanner(t *testing.T) {
	seen := false
	forEachServiceFile(t, func(path string, imports []string) {
		for _, name := range imports {
			if name != gitleaksPkg {
				continue
			}
			if !in(path, "scan") {
				t.Errorf("%s imports the scanner package: only internal/workspace/scan may", path)
			}
			seen = true
		}
	})
	if !seen {
		t.Fatal("nothing imports the scanner package, so the rule checked nothing")
	}
}

// One package connects to a model provider, so that everything sent to one is
// scanned at the moment of sending. And the service never uses the
// command-line tool's own model path, which has no such scan in front of it.
func TestOnlyTheGatewayConnectsToAProvider(t *testing.T) {
	connects := 0
	forEachCall(t, func(path, pkg, function string) {
		if pkg != modelPkg {
			return
		}
		switch function {
		case "NewConverser":
			connects++
			if !in(path, "gateway") {
				t.Errorf("%s calls model.NewConverser: only internal/workspace/gateway may", path)
			}
		case "New":
			t.Errorf("%s calls model.New, the command-line tool's model path: the service reaches a provider through its gateway alone", path)
		}
	})
	if connects == 0 {
		t.Fatal("nothing calls model.NewConverser, so the rule checked nothing")
	}
}

// Provider keys leave the configuration's secrets type through one method, and
// one place calls it: the binary's start-up, which hands them to the gateway.
func TestOnlyTheBinaryRevealsTheKeys(t *testing.T) {
	reveals := 0
	forEachCall(t, func(path, _, function string) {
		if function != "Reveal" {
			return
		}
		reveals++
		if !in(path, "cmd") {
			t.Errorf("%s calls Reveal: only cmd/workspace may take the keys out of their type", path)
		}
	})
	if reveals == 0 {
		t.Fatal("nothing calls Reveal, so the rule checked nothing")
	}
}
