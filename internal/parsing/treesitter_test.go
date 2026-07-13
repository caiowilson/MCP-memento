package parsing

import (
	"strings"
	"testing"
)

func TestAnalyzeSupportedLanguages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path      string
		source    string
		language  string
		wantNames []string
		wantStart int
	}{
		{"fixture.go", "package fixture\n\n// Run executes.\nfunc Run() { println(\"body\") }\n", "go", []string{"Run"}, 3},
		{"fixture.js", "import value from './value.js';\nexport function run() { return value; }\nexport class Worker { start() { return value; } }\n", "javascript", []string{"run", "Worker", "start"}, 2},
		{"fixture.ts", "import type { Value } from './value.js';\nexport interface Runner { run(value: Value): void; }\n", "typescript", []string{"Runner", "run"}, 2},
		{"fixture.tsx", "import React from 'react';\n\nexport function Panel() { return <div />; }\n", "typescript", []string{"Panel"}, 3},
		{"fixture.py", "import os\n\n@decorator\ndef execute(value: str):\n    return value\n", "python", []string{"execute"}, 3},
		{"fixture.rs", "use std::fmt;\n\n#[derive(Debug)]\npub struct Worker { value: usize }\n\nimpl Worker { pub fn run(&self) {} }\n", "rust", []string{"Worker", "run"}, 3},
	}
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			analysis, err := Analyze(test.path, []byte(test.source))
			if err != nil {
				t.Fatal(err)
			}
			if analysis.Language != test.language {
				t.Fatalf("language=%q want %q", analysis.Language, test.language)
			}
			if !containsLine(analysis.DeclarationStarts, test.wantStart) {
				t.Fatalf("starts=%v want %d", analysis.DeclarationStarts, test.wantStart)
			}
			for _, name := range test.wantNames {
				symbol, ok := symbolNamed(analysis.Symbols, name)
				if !ok {
					t.Fatalf("missing %q in %#v", name, analysis.Symbols)
				}
				if symbol.Signature == "" || symbol.ExtentEndLine < symbol.StartLine {
					t.Fatalf("invalid symbol %#v", symbol)
				}
				if name == "execute" && symbol.ExtentStartLine != 3 {
					t.Fatalf("decorated extent starts at %d, want 3", symbol.ExtentStartLine)
				}
			}
		})
	}
}

func containsLine(lines []int, want int) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestAnalyzeMalformedFallsBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path   string
		source string
	}{
		{path: "fixture.go", source: "package fixture\nfunc broken( {\n"},
		{path: "fixture.js", source: "export function broken( {\n"},
		{path: "fixture.ts", source: "export function broken(value: {\n"},
		{path: "fixture.tsx", source: "export function broken( { return <div>;\n"},
		{path: "fixture.py", source: "def broken(:\n"},
		{path: "fixture.rs", source: "pub fn broken( {\n"},
	}
	for _, test := range tests {
		if _, err := Analyze(test.path, []byte(test.source)); err == nil {
			t.Fatalf("expected malformed %s source to fail closed", test.path)
		}
	}
	if Supported("fixture.txt") {
		t.Fatal("unexpected unsupported language")
	}
}

func TestAnalyzeRejectsOversizedSource(t *testing.T) {
	t.Parallel()
	if _, err := Analyze("fixture.go", []byte("package fixture\n"+strings.Repeat(" ", maxSourceBytes))); err == nil {
		t.Fatal("expected oversized source to fail closed")
	}
}

func TestAnalyzeTypeScriptBodiesDoNotBecomeSymbols(t *testing.T) {
	t.Parallel()
	source := []byte("export class Server {\n  public readonly port: number = 3000;\n  name?: string;\n  #token: string;\n  start(): void {\n    const template = `value ${JSON.stringify({ brace: '}' })}`;\n    const matcher = /\\{/;\n    function nestedBodyOnly() {}\n  }\n  status(): { ready: boolean } { return { ready: true }; }\n  #reset(): void {}\n}\n")
	analysis, err := Analyze("fixture.ts", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := symbolNamed(analysis.Symbols, "nestedBodyOnly"); ok {
		t.Fatalf("nested body declaration leaked into symbols: %#v", analysis.Symbols)
	}
	if _, ok := symbolNamed(analysis.Symbols, "template"); ok {
		t.Fatalf("body local leaked into symbols: %#v", analysis.Symbols)
	}
	start, ok := symbolNamed(analysis.Symbols, "start")
	if !ok || start.ExtentEndLine != 9 {
		t.Fatalf("expected complete method extent, got %#v", start)
	}
}

func TestAnalyzeMultilineJavaScriptClass(t *testing.T) {
	t.Parallel()
	source := []byte("export function start(options) {\n  return options.secret;\n}\n\nexport class Worker {\n  run(value) {\n    return value + 'body';\n  }\n}\n")
	analysis, err := Analyze("fixture.js", source)
	if err != nil {
		t.Fatal(err)
	}
	method, ok := symbolNamed(analysis.Symbols, "run")
	if !ok || method.Container != "Worker" || method.ExtentEndLine != 8 {
		t.Fatalf("unexpected method: %#v", method)
	}
}

func TestAnalyzeJavaScriptArrowIsBodyFree(t *testing.T) {
	t.Parallel()
	source := []byte("export const createDefault = (port) => {\n  const secretBodyLocal = 'must-not-leak';\n  return port;\n};\n")
	analysis, err := Analyze("fixture.js", source)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := symbolNamed(analysis.Symbols, "createDefault")
	if !ok || function.Kind != "function" || strings.Contains(function.Signature, "must-not-leak") || function.ExtentEndLine != 4 {
		t.Fatalf("unexpected arrow symbol: %#v", function)
	}
	if _, ok := symbolNamed(analysis.Symbols, "secretBodyLocal"); ok {
		t.Fatalf("arrow body local leaked into symbols: %#v", analysis.Symbols)
	}
}

func symbolNamed(symbols []Symbol, name string) (Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.Name == name {
			return symbol, true
		}
	}
	return Symbol{}, false
}
