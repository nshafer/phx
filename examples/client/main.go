package main

import (
	"bufio"
	"fmt"
	"github.com/nshafer/phx"
	"io"
	"net/url"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run main.go ws[s]://host[:port]/[path][?key=value]")
		os.Exit(1)
	}

	urlStr := os.Args[1]
	endPoint, err := url.Parse(urlStr)
	if err != nil {
		fmt.Println("Invalid url", err)
		os.Exit(1)
	}
	fmt.Printf("Ready to connect to '%v', 'h' for help, 'q' to exit\n", endPoint)

	socket := phx.NewSocket(endPoint)
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

	var channel *phx.Channel

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
			usage()
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
			fmt.Printf("Connection: %v\n", socket.ConnectionState())
			if channel != nil {
				fmt.Printf("Channel: %v\n", channel.State())
			} else {
				fmt.Println("Channel: uninitialized")
			}
		case "p":
			event, payload, _ := strings.Cut(arg, " ")
			if channel != nil && channel.IsJoined() {
				p, err := channel.Push(phx.Event(event), payload)
				if err != nil {
					fmt.Println(err)
					continue
				}
				p.Receive("ok", func(response any) {
					fmt.Println("Push response:", response)
				})
				p.Receive("error", func(response any) {
					fmt.Println("Push error:", response)
				})
			} else {
				fmt.Println("join a channel first")
			}
		case "j":
			topic, paramStr, _ := strings.Cut(arg, " ")
			if topic == "" {
				usage()
				continue
			}
			paramPairs := strings.Split(paramStr, ",")
			params := make(map[string]string)
			for _, pair := range paramPairs {
				key, value, found := strings.Cut(pair, "=")
				if found {
					params[key] = value
				}
			}
			fmt.Printf("Joining %v with %v\n", topic, params)
			channel = socket.Channel(topic, params)
			channel.On(phx.ReplyEvent, func(payload any) {
				fmt.Println("->", payload)
			})
			join, err := channel.Join()
			join.Receive("ok", func(response any) {
				fmt.Println("Connected", response)
				if response == "foobarbaz" {
					fmt.Println("Authenticated!")
				}
			})
			if err != nil {
				fmt.Println(err)
				continue
			} else {
				fmt.Println("JoinPush", join)
			}
		default:
		}
	}
}

func usage() {
	fmt.Print("q: quit\nc: connect\nd: disconnect\nr: reconnect\ns: status\n")
	fmt.Print("j topic [key=value[,...]]\np event [payload]\n")
}
