package main

import (
	"fmt"
	"os"

	"insylus/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "insylus:", err)
		os.Exit(1)
	}
}
