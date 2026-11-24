// SPDX-License-Identifier: MIT

package scip

import "strings"

// Symbol is a parsed symbol identifier.
//
// A symbol is either "local" and an identifier, which means something only
// inside its own document, or five parts: a scheme, a package manager, a
// package name, a version, and a chain of descriptors that leads to the symbol
// inside the package. For example:
//
//	scip-typescript npm rxjs 7.8.2 src/internal/`Observable.ts`/Observable#subscribe().
type Symbol struct {
	// Local marks a symbol scoped to its document. The other fields are empty.
	Local bool
	// Package and Version are the package the symbol belongs to. The format
	// writes "." for none, which is returned here as "".
	Package string
	Version string
	// Names is the name in each descriptor, in order, unescaped.
	Names []string
}

// ConstructorName is the name the indexer gives a class's constructor.
const ConstructorName = "<constructor>"

// LastName is the name in the final descriptor: the symbol's own name.
func (s Symbol) LastName() string {
	if len(s.Names) == 0 {
		return ""
	}
	return s.Names[len(s.Names)-1]
}

// Constructs returns the name of the type a constructor symbol builds, and
// whether the symbol is a constructor at all. `new Foo()` refers to Foo's
// constructor, whose own name is not "Foo".
func (s Symbol) Constructs() (string, bool) {
	if len(s.Names) < 2 || s.LastName() != ConstructorName {
		return "", false
	}
	return s.Names[len(s.Names)-2], true
}

// ParseSymbol reads a symbol identifier. It does not fail: an identifier it
// cannot make sense of comes back with no package and no names, and a lookup
// through it finds nothing, which is the right result for a symbol that cannot
// be understood.
func ParseSymbol(symbol string) Symbol {
	if strings.HasPrefix(symbol, "local ") {
		return Symbol{Local: true}
	}

	// Scheme, manager, package name and version are separated by single
	// spaces. A space inside one of them is written as two.
	var parts []string
	var current strings.Builder
	i := 0
	for i < len(symbol) && len(parts) < 4 {
		if symbol[i] != ' ' {
			current.WriteByte(symbol[i])
			i++
			continue
		}
		if i+1 < len(symbol) && symbol[i+1] == ' ' {
			current.WriteByte(' ')
			i += 2
			continue
		}
		parts = append(parts, current.String())
		current.Reset()
		i++
	}
	if len(parts) < 4 {
		return Symbol{}
	}
	out := Symbol{Package: parts[2], Version: parts[3], Names: descriptorNames(symbol[i:])}
	if out.Package == "." {
		out.Package = ""
	}
	if out.Version == "." {
		out.Version = ""
	}
	return out
}

// descriptorNames returns the name in each descriptor of a chain.
//
// A descriptor is a name followed by a suffix that says what kind of thing it
// names: "/" a namespace, "#" a type, "." a term, ":" a meta, "!" a macro, and
// "(…)." a method, with an optional disambiguator between the brackets. Two
// kinds are written with the name inside brackets instead: "[name]" a type
// parameter and "(name)" a parameter.
//
// Reading stops at the first thing that is not a descriptor. What was read up
// to there is returned.
func descriptorNames(chain string) []string {
	var names []string
	for i := 0; i < len(chain); {
		switch chain[i] {
		case '[':
			name, after, ok := readName(chain, i+1)
			if !ok || after >= len(chain) || chain[after] != ']' {
				return names
			}
			names = append(names, name)
			i = after + 1
		case '(':
			name, after, ok := readName(chain, i+1)
			if !ok || after >= len(chain) || chain[after] != ')' {
				return names
			}
			names = append(names, name)
			i = after + 1
		default:
			name, after, ok := readName(chain, i)
			if !ok || after >= len(chain) {
				return names
			}
			switch chain[after] {
			case '/', '#', '.', ':', '!':
				i = after + 1
			case '(':
				// A method: an optional disambiguator, then ")." to close.
				close := after + 1
				if _, afterName, ok := readName(chain, close); ok {
					close = afterName
				}
				if close+1 >= len(chain) || chain[close] != ')' || chain[close+1] != '.' {
					return names
				}
				i = close + 2
			default:
				return names
			}
			names = append(names, name)
		}
	}
	return names
}

// readName reads one name starting at i: either plain characters, or anything
// between backticks with a backtick inside written twice. It returns the name
// unescaped and the index just past it.
func readName(chain string, i int) (string, int, bool) {
	if i >= len(chain) {
		return "", i, false
	}
	if chain[i] == '`' {
		var out strings.Builder
		for j := i + 1; j < len(chain); j++ {
			if chain[j] != '`' {
				out.WriteByte(chain[j])
				continue
			}
			if j+1 < len(chain) && chain[j+1] == '`' {
				out.WriteByte('`')
				j++
				continue
			}
			if out.Len() == 0 {
				return "", i, false
			}
			return out.String(), j + 1, true
		}
		return "", i, false // no closing backtick
	}
	j := i
	for j < len(chain) && plain(chain[j]) {
		j++
	}
	if j == i {
		return "", i, false
	}
	return chain[i:j], j, true
}

func plain(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '+' || c == '-' || c == '$'
}
