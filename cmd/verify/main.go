package main

import (
	app "github.com/JAIPilot/verify/internal/verify"
	"os"
)

func main() { os.Exit(app.Main(os.Args[1:])) }
