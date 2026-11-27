// SPDX-License-Identifier: MIT

package scip

import (
	"reflect"
	"testing"
)

func TestParseSymbolReadsEveryKindOfDescriptor(t *testing.T) {
	for _, tc := range []struct {
		symbol  string
		pkg     string
		version string
		names   []string
	}{
		// The design's example: namespaces, an escaped file name, a type, a method.
		{"scip-typescript npm rxjs 7.8.2 src/internal/`Observable.ts`/Observable#subscribe().",
			"rxjs", "7.8.2", []string{"src", "internal", "Observable.ts", "Observable", "subscribe"}},
		// A term and a meta.
		{"scip-typescript npm app 1.0.0 src/`config.ts`/defaults.timeout:", "app", "1.0.0", []string{"src", "config.ts", "defaults", "timeout"}},
		// A macro.
		{"scheme manager pkg 1 lib/assert!", "pkg", "1", []string{"lib", "assert"}},
		// A method with a disambiguator, then a parameter.
		{"scip-typescript npm app 1.0.0 `a.ts`/Cart#add(+1).(item)", "app", "1.0.0", []string{"a.ts", "Cart", "add", "item"}},
		// A type parameter.
		{"scip-typescript npm app 1.0.0 `a.ts`/Box#[T]", "app", "1.0.0", []string{"a.ts", "Box", "T"}},
		// A constructor, which the indexer writes inside backticks.
		{"scip-typescript npm app 1.0.0 `a.ts`/Cart#`<constructor>`().", "app", "1.0.0", []string{"a.ts", "Cart", "<constructor>"}},
		// A scoped package name, with characters a plain name does not allow.
		{"scip-typescript npm @scope/pkg 2.0.0-beta.1 `index.d.ts`/thing.", "@scope/pkg", "2.0.0-beta.1", []string{"index.d.ts", "thing"}},
		// No package: the format writes a dot for each missing part.
		{"scip-typescript npm . . `a.ts`/helper().", "", "", []string{"a.ts", "helper"}},
		// A backtick inside an escaped name is written twice.
		{"s m p 1 `odd``name`#", "p", "1", []string{"odd`name"}},
		// A space inside the package name is written as two spaces.
		{"s m my  package 1.0 mod/f().", "my package", "1.0", []string{"mod", "f"}},
		// Names may hold + - $ and digits.
		{"s m p 1 a-b/c+d#$e_1.", "p", "1", []string{"a-b", "c+d", "$e_1"}},
	} {
		got := ParseSymbol(tc.symbol)
		if got.IsLocal || got.Package != tc.pkg || got.Version != tc.version || !reflect.DeepEqual(got.Names, tc.names) {
			t.Errorf("ParseSymbol(%q)\n  = %+v\n  want package %q, version %q, names %q", tc.symbol, got, tc.pkg, tc.version, tc.names)
		}
	}
}

func TestParseSymbolLocal(t *testing.T) {
	got := ParseSymbol("local 42")
	if !got.IsLocal || got.Package != "" || len(got.Names) != 0 || got.LastName() != "" {
		t.Errorf("a local symbol = %+v", got)
	}
	// Only the exact prefix: a package's symbol that happens to start with the
	// letters is not local.
	if ParseSymbol("localscheme npm p 1 a.").IsLocal {
		t.Error("a scheme named localscheme was read as a local symbol")
	}
}

func TestLastNameAndConstructor(t *testing.T) {
	method := ParseSymbol("scip-typescript npm rxjs 7.8.2 src/internal/`Observable.ts`/Observable#subscribe().")
	if method.LastName() != "subscribe" {
		t.Errorf("LastName = %q", method.LastName())
	}
	if _, ok := method.Constructs(); ok {
		t.Error("a method was read as a constructor")
	}

	ctor := ParseSymbol("scip-typescript npm app 1.0.0 `a.ts`/Cart#`<constructor>`().")
	if name, ok := ctor.Constructs(); !ok || name != "Cart" {
		t.Errorf("Constructs = %q, %v; want Cart", name, ok)
	}
	// A constructor with nothing before it constructs nothing nameable.
	if _, ok := ParseSymbol("s m p 1 `<constructor>`().").Constructs(); ok {
		t.Error("a constructor with no type before it")
	}
}

func TestParseSymbolDoesNotFailOnWhatItCannotRead(t *testing.T) {
	for _, symbol := range []string{
		"",
		" ",
		"only three parts",
		"s m p",
		"s m p 1 ",            // no descriptors
		"s m p 1 `unclosed",   // a backtick that never closes
		"s m p 1 ``#",         // an empty escaped name
		"s m p 1 name",        // a name with no suffix
		"s m p 1 a/b#c(",      // a method that never closes
		"s m p 1 a/[T",        // a type parameter that never closes
		"s m p 1 a/(x",        // a parameter that never closes
		"s m p 1 a/\x00\xff#", // bytes that are not a name
		"s m p 1 a/b#!!!",     // suffixes with no names
	} {
		got := ParseSymbol(symbol) // must not panic
		if got.IsLocal {
			t.Errorf("ParseSymbol(%q) is local", symbol)
		}
	}
	// What can be read before the fault is kept.
	if got := ParseSymbol("s m p 1 a/b#c("); !reflect.DeepEqual(got.Names, []string{"a", "b"}) {
		t.Errorf("names before a fault = %q, want a and b", got.Names)
	}
}

func FuzzParseSymbol(f *testing.F) {
	f.Add("scip-typescript npm rxjs 7.8.2 src/internal/`Observable.ts`/Observable#subscribe().")
	f.Add("local 3")
	f.Add("s m p 1 `a``b`#[T](x)f(+1).")
	f.Fuzz(func(t *testing.T, symbol string) {
		got := ParseSymbol(symbol)
		for _, n := range got.Names {
			if n == "" {
				t.Fatalf("ParseSymbol(%q) returned an empty name", symbol)
			}
		}
	})
}
