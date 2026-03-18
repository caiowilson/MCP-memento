package app

import (
	"context"
	"log"
	"os"
	"strings"

	"memento-mcp/internal/mcp"
)

// App represents the application structure.
type App struct{}

// NewApp creates and returns a new instance of App.
func NewApp() *App {
	log.Println("Creating a new App instance")
	return &App{}
}

// Init initializes the application.
func (a *App) Init() {
	log.Println("Initializing the App")
}

// extractServeFlags extracts hidden serve-mode flags such as --root and --child.
func extractServeFlags(args []string) (string, bool, []string) {
	var root string
	var child bool
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--root" && i+1 < len(args) {
			root = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--root=") {
			root = strings.TrimPrefix(a, "--root=")
		} else if a == "--child" {
			child = true
		} else {
			rest = append(rest, a)
		}
	}
	return root, child, rest
}

// extractRootFlag extracts --root=DIR or --root DIR from args, returning
// the root path and remaining args. Returns empty string if not specified.
func extractRootFlag(args []string) (string, []string) {
	root, _, rest := extractServeFlags(args)
	return root, rest
}

// Run is the entrypoint for the application.
func Run() {
	args := os.Args[1:]
	root, child, args := extractServeFlags(args)

	if handled, exitCode := handleCLICommand(args, os.Stdout, os.Stderr); handled {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}

	log.Println("Starting memento-mcp…")

	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
	}

	srv, err := mcp.NewServer(mcp.Config{Root: root, Child: child})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	srv.StartBackgroundIndexing(ctx)

	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
