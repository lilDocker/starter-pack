package main

import (
	"fmt"
	"os"

	"lildocker/builder"
	"lildocker/runtime"
)

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("build <path> <image>")
		fmt.Println("run <image>")
		return
	}

	switch os.Args[1] {

	case "build":

		if len(os.Args) < 4 {
			fmt.Println("build <path> <image>")
			return
		}

		path := os.Args[2]
		image := os.Args[3]

		err := builder.BuildImage(path, image)
		if err != nil {
			fmt.Println("Error:", err)
		}

	case "run":

		if len(os.Args) < 3 {
			fmt.Println("run <image>")
			return
		}

		image := os.Args[2]

		err := runtime.RunContainer(image)
		if err != nil {
			fmt.Println("Error:", err)
		}

	default:
		fmt.Println("Unknown command")
	}
}