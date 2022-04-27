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
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run main.go ws[s]://host[:port]/[path]")
		os.Exit(1)
	}

	fmt.Println("client example running, 'h' for help, 'q' to exit")

	socket := phx.NewSocket(os.Args[1])
	socket.Logger = phx.NewSimpleLogger(phx.LogDebug)
	socket.OnOpen(func() {
		fmt.Println("+ connected")
	})
	socket.OnClose(func() {
		fmt.Println("- disconnected")
	})
	socket.OnError(func(err error) {
		fmt.Println("!", err)
	})
	socket.OnMessage(func(msg phx.Message) {
		fmt.Println("=", msg)
	})

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

		input = strings.Trim(input, " \t\n")
		cmd, arg, _ := strings.Cut(input, " ")

		switch cmd {
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
		case "m":
			socket.Push("none", phx.MessageEvent, arg, 0)
		default:
		}
	}
}
