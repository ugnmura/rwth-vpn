package main

import (
	"os"

	"github.com/ugnmura/rwth-vpn/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
