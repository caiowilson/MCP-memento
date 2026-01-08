package app

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	db "memento-mcp/internal/database"
	"memento-mcp/internal/handlers"
)

type App struct {
	Env *Env
}

type Env struct {
	API_SERVER_PORT string
}

func NewApp() *App {
	return &App{}
}

func (a *App) Init() {
	a.SetupEnv()
	a.SetupServer()
}

func (a *App) SetupEnv() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal(err)
	}

	a.Env = &Env{
		API_SERVER_PORT: os.Getenv("API_SERVER_PORT"),
	}
}

func (a *App) SetupServer() {

	db := db.New()

	h := handlers.New(db)
	h.SetupRoutes()

	err := h.StartServer()
	if err != nil {
		log.Printf("Error Starting Server:\nMessage:\n%v", err.Error())
	}
}
