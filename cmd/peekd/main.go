package main

import (
	"os"

	"github.com/puemos/peek/internal/peekd"
)

func main() {
	os.Exit(peekd.Run(os.Args[1:]))
}
