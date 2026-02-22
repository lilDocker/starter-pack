package main

import (
	"fmt"
	"os"

	"github.com/you/lildocker/cmd"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		cmd.Build(os.Args[2:])
	case "run":
		cmd.Run(os.Args[2:])
	case "images":
		cmd.Images()
	case "ps":
		cmd.Ps()
	case "stop":
		cmd.Stop(os.Args[2:])
	case "rm":
		cmd.Rm(os.Args[2:])
	case "rmi":
		cmd.Rmi(os.Args[2:])
	case "exec":
		cmd.Exec(os.Args[2:])
	case "logs":
		cmd.Logs(os.Args[2:])
	case "network":
		cmd.Network(os.Args[2:])
	case "_child": // internal: re-exec into container namespace
		cmd.Child(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`
lildocker - a minimal container runtime

Usage:
  lildocker build -t <name> [path]   Build an image from a lilDockfile
  lildocker run   <image> [cmd]      Run a container from an image
  lildocker images                   List all images
  lildocker ps                       List running containers
  lildocker stop  <container-id>     Stop a running container
  lildocker rm    <container-id>     Remove a container
  lildocker rmi   <image-name>       Remove an image
  lildocker exec  <container-id> cmd Run command in running container
  lildocker logs  <container-id>     Show container logs
  lildocker network ls               List networks
  lildocker network create <name>    Create a network

lilDockfile Example:
  FROM alpine
  WORKDIR /app
  COPY . /app
  ENV APP_PORT=8080
  RUN apk add --no-cache curl
  EXPOSE 8080
  CMD ["/app/myapp"]
`)
}
