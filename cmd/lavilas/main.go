package main

import (
	"os"

	"github.com/Perdonus/lavilas-code/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
