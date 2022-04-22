package main

import (
	"bufio"
	"fmt"
	"github.com/nshafer/phx"
	"io"
	"os"
	"strings"
)

func main() {
	fmt.Println("client example running, 'h' for help, 'q' to exit")

	if len(os.Args) != 2 {
		fmt.Println("Usage: go run main.go ws[s]://host[:port]/[path]")
		os.Exit(1)
	}

	socket := phx.NewSocket(os.Args[1])
	socket.Logger = phx.NewSimpleLogger(phx.LogDebug)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")

		input, err := reader.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}

		switch strings.Trim(input, " \t\n") {
		case "h":
			fmt.Print("q: quit\nc: connect\nd: disconnect\nr: reconnect\ns: status\n")
		case "q":
			return
		case "c":
			if err := socket.Connect(); err != nil {
				fmt.Println(err)
			}
		case "d":
			if err := socket.Disconnect(); err != nil {
				fmt.Println(err)
			}
		case "r":
			if err := socket.Reconnect(); err != nil {
				fmt.Println(err)
			}
		case "s":
			fmt.Printf("Connected: %v\n", socket.IsConnected())
		default:
		}
	}
}
