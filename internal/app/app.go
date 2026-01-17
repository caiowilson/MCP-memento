package app

import (
	"context"
	"log"
	"os"

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

// Run is the entrypoint for the application.
func Run() {
	log.Println("Starting memento-mcp…")

	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	srv, err := mcp.NewServer(mcp.Config{Root: root})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	srv.StartBackgroundIndexing(ctx)

	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
