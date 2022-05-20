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
		fmt.Println("Usage: go run cli.go ws[s]://host[:port]/[path][?key=value]")
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
	//socket.Serializer = phx.NewJSONSerializerV1()
	socket.Logger = phx.NewSimpleLogger(phx.LogInfo)
	socket.OnOpen(func() {
		fmt.Println("+ connected")
	})
	socket.OnClose(func() {
		fmt.Println("x disconnected")
	})
	socket.OnError(func(err error) {
		fmt.Printf("! %v\n", err)
	})
	socket.OnMessage(func(msg phx.Message) {
		fmt.Printf("< %+v\n", msg)
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

		case "ch":
			if channel != nil {
				err := channel.Remove()
				if err != nil {
					fmt.Println(err)
					continue
				}
			}

			topic, paramStr, _ := strings.Cut(arg, " ")
			if topic == "" {
				usage()
				continue
			}
			paramPairs := strings.Split(paramStr, ",")
			params := make(map[string]string)
			for _, pair := range paramPairs {
				key, value, found := strings.Cut(pair, ":")
				if found {
					params[key] = value
				}
			}
			fmt.Printf("Creating channel %v with %v\n", topic, params)
			channel = socket.Channel(topic, params)
			channel.On(string(phx.ReplyEvent), func(payload any) {
				fmt.Println("<-", payload)
			})
			channel.OnClose(func(payload any) {
				fmt.Println("x-", payload)
			})
			channel.OnError(func(payload any) {
				fmt.Println("!-", payload)
			})
			channel.On("shout", func(payload any) {
				fmt.Println("s-", payload)
			})

		case "rm":
			if channel != nil {
				err := channel.Remove()
				if err != nil {
					fmt.Println(err)
					continue
				}
			} else {
				fmt.Println("Cannot remove non-existant channel")
			}

		case "j":
			if channel != nil {
				join, err := channel.Join()
				if err != nil {
					fmt.Println(err)
					continue
				}
				join.Receive("ok", func(response any) {
					fmt.Println("Joined channel:", channel.Topic(), response)
				})
				join.Receive("error", func(response any) {
					fmt.Println("Join error", response)
				})
				//fmt.Println("JoinPush", join)
			} else {
				fmt.Println("Create a channel first")
			}

		case "l":
			if channel != nil {
				leave, err := channel.Leave()
				if err != nil {
					fmt.Println(err)
					continue
				}
				leave.Receive("ok", func(response any) {
					fmt.Println("Left channel:", channel.Topic(), response)
				})
				leave.Receive("error", func(response any) {
					fmt.Println("Leave error:", response)
				})
			} else {
				fmt.Println("Create a channel first")
			}

		case "rj":
			if channel != nil {
				leave, err := channel.Leave()
				if err != nil {
					fmt.Println(err)
					continue
				}

				leave.Receive("ok", func(response any) {
					fmt.Println("Left channel:", channel.Topic(), response)
					join, err := channel.Join()
					if err != nil {
						fmt.Println(err)
						return
					}
					join.Receive("ok", func(response any) {
						fmt.Println("Joined channel:", channel.Topic(), response)
					})
					join.Receive("error", func(response any) {
						fmt.Println("Join error", response)
					})
				})
			} else {
				fmt.Println("Create a channel first")
			}

		case "p":
			event, payloadStr, _ := strings.Cut(arg, " ")
			if channel != nil {
				var payload any

				// check if payloadStr looks like a map
				if strings.Contains(payloadStr, ":") {
					payloadMap := make(map[string]string)
					payloadPairs := strings.Split(payloadStr, ",")
					for _, pair := range payloadPairs {
						key, value, found := strings.Cut(pair, ":")
						if found {
							payloadMap[key] = value
						}
					}
					payload = payloadMap
				} else {
					payload = payloadStr
				}

				p, err := channel.Push(event, payload)
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
				p.Receive("timeout", func(response any) {
					fmt.Println("Push timeout:", response)
				})
			} else {
				fmt.Println("Create a channel first")
			}

		default:
			if cmd != "" {
				fmt.Println("Unknown command")
				usage()
				continue
			}
		}
	}
}

func usage() {
	fmt.Print(`
q                               quit
c                               connect
d                               disconnect
r                               reconnect
s                               status
ch topic [key:value[,...]]      create channel
rm                              remove channel
j                               join channel
l                               leave channel
rj                              rejoin channel
p event [payload]               push event
`)
}
