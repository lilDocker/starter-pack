package main

import (
	"fmt"
	"os"
)

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Usage: lilDocker <command>")
		return
	}

	switch os.Args[1] {

	case "run":
		fmt.Println("Running container...")

	case "build":
		fmt.Println("Building image...")

	default:
		fmt.Println("Unknown command")
	}
}