# Navigation test data

A small TypeScript project, two indexes of it, and the answers the lookup rules must give.

| File | What it is |
| --- | --- |
| `tsproject/` | The project. It holds every case the lookup rules name: a name that is both a type and a value (`Money`), import paths, a class with a constructor (`Cart`), a namespace import (`tax`), a dependency (`tiny-format`), a character outside the basic plane before an identifier, and a file that is not TypeScript. |
| `index.full.scip` | The index `scip-typescript` 0.6.2 builds for the whole project. |
| `index.partial.scip` | The index it builds for `tsconfig.partial.json`, which covers two files. Symbols from the rest of the project have no definition in it. |
| `golden.full.jsonl`, `golden.partial.jsonl` | For the start of every occurrence in each index, the label and targets the rules give. Written by the reference implementation, `docs/designs/trials/scip_lookup.py golden`. |
| `compiler.jsonl` | Where the TypeScript compiler says each identifier is defined. Written by `docs/designs/trials/compiler_truth.cjs`. |

The tests hold the Go implementation to the golden files exactly, and check separately that no
answer contradicts the compiler.

## Do not edit the project's files by hand

Every position in the indexes, the golden files and the compiler's answers counts lines and
characters in these files. A changed line moves them. That is also why the files carry no licence
header. To change the project, change it and rebuild everything below.

## Rebuilding

An index records the path it was built at. Build at this fixed path, so that the index holds no
contributor's own directory. A test fails if it was built anywhere else.

```
B=/tmp/review-workspace-fixture
rm -rf $B && mkdir -p $B
cp -R internal/workspace/navigation/testdata/tsproject $B/tsproject
cd $B/tsproject

# The dependency, made by hand. It is needed only while indexing, and is not kept.
mkdir -p node_modules/tiny-format
echo '{ "name": "tiny-format", "version": "2.1.0", "main": "index.js", "types": "index.d.ts" }' \
  > node_modules/tiny-format/package.json
echo 'export declare function format(cents: number): string;' > node_modules/tiny-format/index.d.ts
echo 'exports.format = (c) => (c / 100).toFixed(2);' > node_modules/tiny-format/index.js
npm install --no-save --no-audit --no-fund typescript@5.9.3

npx --yes @sourcegraph/scip-typescript@0.6.2 index --output index.scip
npx --yes @sourcegraph/scip-typescript@0.6.2 index tsconfig.partial.json --output index.partial.scip
```

Then, from the repository root:

```
T=$PWD/docs/designs/trials
D=$PWD/internal/workspace/navigation/testdata
cd /tmp/review-workspace-fixture/tsproject
cp index.scip $D/index.full.scip
cp index.partial.scip $D/index.partial.scip
python3 $T/scip_lookup.py golden index.scip . > $D/golden.full.jsonl
python3 $T/scip_lookup.py golden index.partial.scip . > $D/golden.partial.jsonl
cp $T/compiler_truth.cjs . && node compiler_truth.cjs 1 > $D/compiler.jsonl
python3 $T/compare_with_compiler.py index.scip . $D/compiler.jsonl
```

The last command prints the score. As built on 2026-11-24, under the revised rules: 56 of 66
identifiers resolved to where the compiler says, 7 were placed outside the repository as the
compiler places them, 3 were not resolved and fall back to text search, and none was wrong.
