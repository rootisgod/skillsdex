package main

import (
	"os"

	"skillsdex"
)

func main() {
	os.Exit(skillsdex.RunCLI(os.Args[1:]))
}
