package main

import (
	"os"

	"skillsdex"
)

func main() {
	os.Exit(skillsdex.RunDaemon(os.Args[1:]))
}
