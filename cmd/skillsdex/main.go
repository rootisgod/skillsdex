package main

import (
	"os"

	"github.com/rootisgod/skillsdex"
)

func main() {
	os.Exit(skillsdex.RunCLI(os.Args[1:]))
}
