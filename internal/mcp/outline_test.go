package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoOutline(t *testing.T) {
	dir := t.TempDir()
	src := `package example

import "fmt"

// Server handles HTTP requests.
type Server struct {
	addr    string
	port    int
	handler Handler
}

// Handler processes requests.
type Handler interface {
	Handle(path string) error
	Close() error
}

// NewServer creates a new Server.
func NewServer(addr string, port int) *Server {
	fmt.Println("creating")
	return &Server{addr: addr, port: port}
}

// Start begins serving.
func (s *Server) Start() error {
	fmt.Println("starting")
	return nil
}

const DefaultPort = 8080

var Version string
`
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outline, err := goOutline(path)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		desc    string
		want    string
		present bool
	}{
		{"package", "package example", true},
		{"Server struct", "type Server struct", true},
		{"Handler interface", "type Handler interface", true},
		{"Handle method", "Handle(path string) error", true},
		{"Close method", "Close() error", true},
		{"NewServer func", "NewServer", true},
		{"Start method", "Start", true},
		{"DefaultPort const", "const DefaultPort", true},
		{"Version var", "var Version", true},
		{"doc comment", "Server handles HTTP", true},
		{"no body return nil", "return nil", false},
		{"no body fmt", "fmt.Println", false},
		{"no import", `import "fmt"`, false},
	}
	for _, c := range checks {
		has := strings.Contains(outline, c.want)
		if c.present && !has {
			t.Errorf("%s: expected outline to contain %q\nGot:\n%s", c.desc, c.want, outline)
		}
		if !c.present && has {
			t.Errorf("%s: expected outline NOT to contain %q\nGot:\n%s", c.desc, c.want, outline)
		}
	}
}

func TestGoSummary(t *testing.T) {
	dir := t.TempDir()
	src := `package example

type Server struct {
	addr string
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

func (s *Server) Start() error {
	return nil
}
`
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := goSummary(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(summary, "package example") {
		t.Errorf("missing package\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "type Server struct") {
		t.Errorf("missing Server struct\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "func NewServer") {
		t.Errorf("missing NewServer\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "Start") {
		t.Errorf("missing Start method\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "L") {
		t.Errorf("summary should contain line numbers\nGot:\n%s", summary)
	}
	if strings.Contains(summary, "return") {
		t.Errorf("summary should not contain function bodies\nGot:\n%s", summary)
	}
}

func TestJsOutline(t *testing.T) {
	dir := t.TempDir()
	src := `/**
 * Create a server instance.
 */
export function createServer(port) {
    return { port };
}

export class Server {
    constructor(port) {
        this.port = port;
    }
}

export const DEFAULT_PORT = 8080;

function helperInternal() {
    // ...
}
`
	path := filepath.Join(dir, "server.js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outline, err := jsOutline(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(outline, "export function createServer") {
		t.Errorf("missing createServer\nGot:\n%s", outline)
	}
	if !strings.Contains(outline, "export class Server") {
		t.Errorf("missing Server class\nGot:\n%s", outline)
	}
	if !strings.Contains(outline, "export const DEFAULT_PORT") {
		t.Errorf("missing DEFAULT_PORT\nGot:\n%s", outline)
	}
	if !strings.Contains(outline, "Create a server instance") {
		t.Errorf("missing JSDoc comment\nGot:\n%s", outline)
	}
	if strings.Contains(outline, "this.port") {
		t.Errorf("outline should not contain implementation\nGot:\n%s", outline)
	}
}

func TestJsSummary(t *testing.T) {
	dir := t.TempDir()
	src := `export function createServer(port) {
    return { port };
}

export class Server {
}

export const DEFAULT_PORT = 8080;
`
	path := filepath.Join(dir, "server.js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := jsSummary(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(summary, "L1:") {
		t.Errorf("missing line number for createServer\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "export function createServer") {
		t.Errorf("missing createServer\nGot:\n%s", summary)
	}
	if !strings.Contains(summary, "export class Server") {
		t.Errorf("missing Server class\nGot:\n%s", summary)
	}
}

func TestGenericOutline(t *testing.T) {
	dir := t.TempDir()
	src := `# Python-like file

class MyClass:
    def __init__(self):
        pass

def helper_function():
    return 42

class AnotherClass:
    pass
`
	path := filepath.Join(dir, "example.py")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outline, err := genericOutline(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(outline, "class MyClass") {
		t.Errorf("missing MyClass\nGot:\n%s", outline)
	}
	if !strings.Contains(outline, "def helper_function") {
		t.Errorf("missing helper_function\nGot:\n%s", outline)
	}
	if !strings.Contains(outline, "class AnotherClass") {
		t.Errorf("missing AnotherClass\nGot:\n%s", outline)
	}
}
