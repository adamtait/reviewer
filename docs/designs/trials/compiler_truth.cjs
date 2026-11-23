// SPDX-License-Identifier: MIT
//
// Trial tool: ask the TypeScript compiler where a sample of identifiers is defined.
// Run it in a TypeScript project that has `typescript` installed:
//
//   node compiler_truth.cjs [EVERY_NTH] > truth.jsonl
//
// Each output line is one identifier with the compiler's definitions for it.
// compare_with_compiler.py scores an index against these answers.
const ts = require("typescript"), path = require("path"), fs = require("fs");
const root = process.cwd(), every = Number(process.argv[2] || 7);
const cfgPath = ts.findConfigFile(root, ts.sys.fileExists, "tsconfig.json");
const cfg = ts.parseJsonConfigFileContent(ts.readConfigFile(cfgPath, ts.sys.readFile).config, ts.sys, root);
const files = cfg.fileNames;
const host = { getScriptFileNames: () => files, getScriptVersion: () => "1",
  getScriptSnapshot: f => fs.existsSync(f) ? ts.ScriptSnapshot.fromString(fs.readFileSync(f, "utf8")) : undefined,
  getCurrentDirectory: () => root, getCompilationSettings: () => cfg.options, getDefaultLibFileName: o => ts.getDefaultLibFilePath(o),
  fileExists: ts.sys.fileExists, readFile: ts.sys.readFile, readDirectory: ts.sys.readDirectory, directoryExists: ts.sys.directoryExists, getDirectories: ts.sys.getDirectories };
const ls = ts.createLanguageService(host, ts.createDocumentRegistry());
const program = ls.getProgram(); let n = 0;
for (const f of files) {
  const sf = program.getSourceFile(f); if (!sf) continue;
  const visit = node => {
    if (ts.isIdentifier(node) && !(node.parent && node.parent.name === node && (ts.isFunctionDeclaration(node.parent) || ts.isVariableDeclaration(node.parent) || ts.isClassDeclaration(node.parent) || ts.isInterfaceDeclaration(node.parent) || ts.isTypeAliasDeclaration(node.parent) || ts.isParameter(node.parent) || ts.isPropertySignature(node.parent) || ts.isMethodDeclaration(node.parent) || ts.isPropertyDeclaration(node.parent) || ts.isEnumDeclaration(node.parent) || ts.isPropertyAssignment(node.parent)))) {
      if (n++ % every === 0) {
        const start = node.getStart(sf), lc = sf.getLineAndCharacterOfPosition(start);
        const defs = (ls.getDefinitionAtPosition(f, start) || []).map(d => { const dsf = program.getSourceFile(d.fileName); const dl = dsf ? dsf.getLineAndCharacterOfPosition(d.textSpan.start) : { line: -1, character: -1 };
          return { path: path.relative(root, d.fileName), line: dl.line, character: dl.character, kind: d.kind }; });
        console.log(JSON.stringify({ path: path.relative(root, f), line: lc.line, character: lc.character, text: node.text, defs }));
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}
