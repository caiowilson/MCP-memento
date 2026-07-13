package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredGoOutlineReturnsCompleteSignaturesWithoutBodies(t *testing.T) {
	source := []byte(`// Package fixture documents the package.
package fixture

import (
	"context"
	alias "example.com/dependency"
)

// Server serves requests.
type Server struct {
	Address string
	Port int
}

// Runner executes work.
type Runner interface {
	Run(context.Context) error
}

const DefaultPort int = 8080

// NewServer constructs a server.
func NewServer(
	address string,
	options ...Option,
) (*Server, error) {
	bodySentinel := "go-body-must-not-appear"
	_ = bodySentinel
	return nil, nil
}

// Start begins serving.
func (s *Server) Start(ctx context.Context) error {
	return ctx.Err()
}
`)
	outline := extractStructuredFileOutline("fixture.go", source)
	if outline.Language != "go" || outline.PackageName != "fixture" || outline.Fallback {
		t.Fatalf("unexpected Go outline metadata: %#v", outline)
	}
	if !containsString(outline.Imports, "context") || !containsString(outline.Imports, "alias example.com/dependency") {
		t.Fatalf("unexpected imports: %#v", outline.Imports)
	}
	server := requireOutlineSymbol(t, outline.Symbols, "Server", "struct")
	if !strings.Contains(server.Signature, "Address string") || !strings.Contains(server.Signature, "Port int") {
		t.Fatalf("expected complete struct fields, got %q", server.Signature)
	}
	runner := requireOutlineSymbol(t, outline.Symbols, "Runner", "interface")
	if !strings.Contains(runner.Signature, "Run(context.Context) error") {
		t.Fatalf("expected complete interface signature, got %q", runner.Signature)
	}
	constructor := requireOutlineSymbol(t, outline.Symbols, "NewServer", "function")
	if !strings.Contains(constructor.Signature, "address string") || !strings.Contains(constructor.Signature, "options ...Option") || !strings.Contains(constructor.Signature, "(*Server, error)") {
		t.Fatalf("expected complete multiline function signature, got %q", constructor.Signature)
	}
	if constructor.Documentation != "NewServer constructs a server." {
		t.Fatalf("unexpected documentation: %q", constructor.Documentation)
	}
	method := requireOutlineSymbol(t, outline.Symbols, "Start", "method")
	if method.Container != "*Server" {
		t.Fatalf("expected method receiver, got %#v", method)
	}
	assertOutlineOmits(t, outline, "go-body-must-not-appear", "return ctx.Err()")
}

func TestStructuredTypeScriptOutlineReturnsMembersWithoutBodies(t *testing.T) {
	source := []byte(`import { Dependency } from "./dependency";
// import "./commented";

/** Creates a server. */
export async function createServer(
  dependency: Dependency,
  port: number,
): Promise<Server> {
  const bodySentinel = "ts-body-must-not-appear";
  return new Server(port);
}

export function describe(
  { name }: { name: string },
): { label: string; count: number } {
  return { label: name, count: 1 };
}

export interface Options {
  port: number;
  secure?: boolean;
}

/** Server coordinates requests. */
export class Server {
  public readonly port: number = 3000;
  name?: string;
  #token: string;

  /** Starts the server. */
  public async start(
    signal?: AbortSignal,
  ): Promise<void> {
	const template = ` + "`value ${JSON.stringify({ brace: '}' })}`" + `;
	const matcher = /\{/;
	const closingMatcher = /}/;
	const bodyOnly = require("./body-only");
    console.log(template, signal);
  }

  stop(): void {
    console.log("second-body-must-not-appear");
  }

  status(): { ready: boolean } {
    return { ready: true };
  }

  #reset(): void {
    function nestedBodyOnly() {}
  }
}

export const createDefault = (port: number): Server => {
  return new Server(port);
};
`)
	outline := extractStructuredFileOutline("fixture.ts", source)
	if outline.Language != "typescript" {
		t.Fatalf("unexpected TypeScript metadata: %#v", outline)
	}
	if !containsString(outline.Imports, "./dependency") {
		t.Fatalf("expected dependency import, got %#v", outline.Imports)
	}
	if containsString(outline.Imports, "./commented") || containsString(outline.Imports, "./body-only") {
		t.Fatalf("expected only top-level code imports, got %#v", outline.Imports)
	}
	function := requireOutlineSymbol(t, outline.Symbols, "createServer", "function")
	if !strings.Contains(function.Signature, "dependency: Dependency") || !strings.Contains(function.Signature, "port: number") || !strings.Contains(function.Signature, "Promise<Server>") {
		t.Fatalf("expected complete multiline function signature, got %q", function.Signature)
	}
	describe := requireOutlineSymbol(t, outline.Symbols, "describe", "function")
	if !strings.Contains(describe.Signature, "{ name }: { name: string }") || !strings.Contains(describe.Signature, "{ label: string; count: number }") {
		t.Fatalf("expected inline object types in complete signature, got %q", describe.Signature)
	}
	options := requireOutlineSymbol(t, outline.Symbols, "Options", "interface")
	if !strings.Contains(options.Signature, "secure?: boolean") {
		t.Fatalf("expected complete interface definition, got %q", options.Signature)
	}
	requireOutlineSymbol(t, outline.Symbols, "port", "property")
	requireOutlineSymbol(t, outline.Symbols, "name", "property")
	requireOutlineSymbol(t, outline.Symbols, "#token", "property")
	start := requireOutlineSymbol(t, outline.Symbols, "start", "method")
	if start.Container != "Server" || !strings.Contains(start.Signature, "signal?: AbortSignal") || !strings.Contains(start.Signature, "Promise<void>") {
		t.Fatalf("unexpected class method: %#v", start)
	}
	requireOutlineSymbol(t, outline.Symbols, "stop", "method")
	requireOutlineSymbol(t, outline.Symbols, "#reset", "method")
	status := requireOutlineSymbol(t, outline.Symbols, "status", "method")
	if !strings.Contains(status.Signature, "{ ready: boolean }") {
		t.Fatalf("expected object return type in method signature, got %q", status.Signature)
	}
	requireOutlineSymbol(t, outline.Symbols, "createDefault", "function")
	assertOutlineOmits(t, outline, "ts-body-must-not-appear", "second-body-must-not-appear", "nestedBodyOnly", "console.log", "return new Server")
}

func TestStructuredPHPOutlineReturnsNamespaceAndMembersWithoutBodies(t *testing.T) {
	source := []byte(`<?php
namespace App\Http;

use Psr\Log\LoggerInterface;
require_once './bootstrap.php';
// require './commented.php';

/** Handles requests. */
final class Controller extends BaseController
{
    private readonly LoggerInterface $logger;

    /** Dispatches one request. */
    public function dispatch(
        Request $request,
        ?User $user = null,
    ): Response {
        $bodySentinel = 'php-body-must-not-appear';
        $template = <<<HTML
}
function nested_body_only() {}
HTML;
        require './body.php';
        return new Response();
    }
}

function helper(string $value): string {
    return strtoupper($value);
}
`)
	outline := extractStructuredFileOutline("fixture.php", source)
	if outline.Language != "php" || outline.PackageName != `App\Http` || outline.Fallback {
		t.Fatalf("unexpected PHP metadata: %#v", outline)
	}
	if !containsString(outline.Imports, "./bootstrap.php") || !containsString(outline.Imports, `Psr\Log\LoggerInterface`) {
		t.Fatalf("unexpected PHP imports: %#v", outline.Imports)
	}
	if containsString(outline.Imports, "./commented.php") || containsString(outline.Imports, "./body.php") {
		t.Fatalf("expected only top-level PHP imports, got %#v", outline.Imports)
	}
	controller := requireOutlineSymbol(t, outline.Symbols, "Controller", "class")
	if !strings.Contains(controller.Signature, "extends BaseController") {
		t.Fatalf("expected complete class header, got %q", controller.Signature)
	}
	requireOutlineSymbol(t, outline.Symbols, "logger", "property")
	dispatch := requireOutlineSymbol(t, outline.Symbols, "dispatch", "method")
	if dispatch.Container != "Controller" || !strings.Contains(dispatch.Signature, "Request $request") || !strings.Contains(dispatch.Signature, "): Response") {
		t.Fatalf("expected complete PHP method signature, got %#v", dispatch)
	}
	requireOutlineSymbol(t, outline.Symbols, "helper", "function")
	assertOutlineOmits(t, outline, "php-body-must-not-appear", "nested_body_only", "return new Response", "strtoupper")
}

func TestStructuredJavaScriptOutlineUsesJavaScriptLanguage(t *testing.T) {
	source := []byte(`export function start(options) {
  return options.secret;
}

export class Worker {
  run(value) {
    return value + "js-body-must-not-appear";
  }
}
`)
	outline := extractStructuredFileOutline("fixture.js", source)
	if outline.Language != "javascript" || outline.Fallback {
		t.Fatalf("unexpected JavaScript metadata: %#v", outline)
	}
	requireOutlineSymbol(t, outline.Symbols, "start", "function")
	run := requireOutlineSymbol(t, outline.Symbols, "run", "method")
	if run.Container != "Worker" {
		t.Fatalf("expected JavaScript method container, got %#v", run)
	}
	assertOutlineOmits(t, outline, "js-body-must-not-appear", "return options.secret")
}

func TestStructuredTreeSitterOutlinesCoverTSXPythonAndRust(t *testing.T) {
	tests := []struct {
		path       string
		source     string
		language   string
		name       string
		kind       string
		container  string
		bodyMarker string
	}{
		{
			path:       "panel.tsx",
			source:     "export class Panel {\n  render(): JSX.Element {\n    return <section>tsx-body-marker</section>;\n  }\n}\n",
			language:   "typescript",
			name:       "render",
			kind:       "method",
			container:  "Panel",
			bodyMarker: "tsx-body-marker",
		},
		{
			path:       "worker.py",
			source:     "import os\n\n# Runs work.\nclass Worker:\n    def run(self, value: str) -> str:\n        return value + 'python-body-marker'\n",
			language:   "python",
			name:       "run",
			kind:       "method",
			container:  "Worker",
			bodyMarker: "python-body-marker",
		},
		{
			path:       "worker.rs",
			source:     "use std::fmt;\n\npub struct Worker;\n\nimpl Worker {\n    pub fn run(&self) -> bool {\n        let _marker = \"rust-body-marker\";\n        true\n    }\n}\n",
			language:   "rust",
			name:       "run",
			kind:       "method",
			container:  "Worker",
			bodyMarker: "rust-body-marker",
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			outline := extractStructuredFileOutline(test.path, []byte(test.source))
			if outline.Language != test.language || outline.Fallback {
				t.Fatalf("unexpected outline metadata: %#v", outline)
			}
			symbol := requireOutlineSymbol(t, outline.Symbols, test.name, test.kind)
			if symbol.Container != test.container || symbol.ExtentEndLine <= symbol.EndLine {
				t.Fatalf("expected container and full body extent, got %#v", symbol)
			}
			assertOutlineOmits(t, outline, test.bodyMarker)
		})
	}
}

func TestStructuredSupportedLanguageParseFailureUsesSafeFallback(t *testing.T) {
	tests := []struct {
		path   string
		source string
	}{
		{path: "broken.py", source: "def broken(:\n    return 'python-fallback-body'\n"},
		{path: "broken.rs", source: "pub fn broken( {\n    let marker = \"rust-fallback-body\";\n}\n"},
		{path: "broken.php", source: "<?php\nclass Broken {\n    public function nope( {\n        return 'php-fallback-body';\n    }\n}\n"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			outline := extractStructuredFileOutline(test.path, []byte(test.source))
			if !outline.Fallback {
				t.Fatalf("expected parse failure fallback, got %#v", outline)
			}
			assertOutlineOmits(t, outline, "fallback-body")
		})
	}
}

func TestStructuredBladeTemplateUsesPHPScannerFallback(t *testing.T) {
	outline := extractStructuredFileOutline("resources/views/dashboard.blade.php", []byte("@extends('layouts.app')\n<div>{{ $value }}</div>\n"))
	if outline.Language != "php" || !outline.Fallback {
		t.Fatalf("expected bounded PHP fallback for Blade, got %#v", outline)
	}
}

func TestStructuredGenericOutlineDegradesWithoutInlineBodies(t *testing.T) {
	source := []byte("#!/usr/bin/env ruby\n\nclass Worker: pass\n\ndef execute(value: str): return value.upcase\n")
	outline := extractStructuredFileOutline("worker.rb", source)
	if outline.Language != "ruby" || !outline.Fallback {
		t.Fatalf("expected Ruby fallback, got %#v", outline)
	}
	worker := requireOutlineSymbol(t, outline.Symbols, "Worker", "class")
	if worker.Signature != "class Worker:" {
		t.Fatalf("expected class body removed, got %q", worker.Signature)
	}
	execute := requireOutlineSymbol(t, outline.Symbols, "execute", "def")
	if execute.Signature != "def execute(value: str):" {
		t.Fatalf("expected inline function body removed, got %q", execute.Signature)
	}
	assertOutlineOmits(t, outline, "return value.upcase")
}

func TestStructuredGenericHeaderStopsAtFirstDeclaration(t *testing.T) {
	source := []byte("#!/usr/bin/env ruby\ndef execute():\n    return 'fallback-body-must-not-appear'\n")
	outline := extractStructuredFileOutline("worker.rb", source)
	if len(outline.Header) != 2 || outline.Header[1] != "def execute():" {
		t.Fatalf("expected bounded pre-declaration header, got %#v", outline.Header)
	}
	assertOutlineOmits(t, outline, "fallback-body-must-not-appear")
}

func TestStructuredDocumentationMustImmediatelyPrecedeDeclaration(t *testing.T) {
	source := []byte("/** Applies to something else. */\n\nexport function start(): void {}\n")
	outline := extractStructuredFileOutline("fixture.ts", source)
	symbol := requireOutlineSymbol(t, outline.Symbols, "start", "function")
	if symbol.Documentation != "" {
		t.Fatalf("expected blank line to break documentation association, got %q", symbol.Documentation)
	}
}

func requireOutlineSymbol(t *testing.T, symbols []outlineSymbol, name, kind string) outlineSymbol {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind {
			return symbol
		}
	}
	t.Fatalf("missing %s symbol %q in %#v", kind, name, symbols)
	return outlineSymbol{}
}

func assertOutlineOmits(t *testing.T, outline structuredFileOutline, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(outline)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("outline unexpectedly contains %q: %s", value, encoded)
		}
	}
}
