// SPDX-License-Identifier: MIT

package workspace_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// serviceDirs are the trees that make up the service's own code.
var serviceDirs = []string{".", filepath.Join("..", "..", "cmd", "workspace")}

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
