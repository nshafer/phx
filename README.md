# Phx - A Phoenix Channels client for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/nshafer/phx.svg)](https://pkg.go.dev/github.com/nshafer/phx)

This is a comprehensive client for Phoenix Channels written for Go applications.
The goal of this project is to be a reliable, resilient, full-featured client library for connecting to Phoenix
servers over websockets and being able to push and receive events from one or more channels in a performant and
concurrent manner.

This module is based off of the official JavaScript client implementation, except where deviations were needed due to
being written in Go. But if you're familiar with connecting to Phoenix Channels in JS, then this library should feel
right at home.

## Installation

    go get github.com/nshafer/phx

## Documentation

API documentation can be viewed via godoc at [API Documentation](https://pkg.go.dev/github.com/nshafer/phx).

Examples are in [examples/](examples/).

## Features

- Supports websockets as the transport method. Longpoll is not currently supported, nor are there plans to implement it.
- Supports the JSONSerializerV2 serializer. (JSONSerializerV1 also available if preferred.)
- All event handlers are simple functions that are registered with the Socket, Channels or Pushes. No complicated
  interfaces to implement.
- Completely concurrent using many goroutines in the background so that your main thread is not blocked. All callbacks
  will run in separate goroutines.
- Supports setting connection parameters, headers, proxy, etc on the main websocket connection.
- Supports passing parameters when joining a Channel
- Pluggable Transport, TransportHandler, Logger if needed.

## Simple example

For a more complete example, see [examples/simple/simple.go](examples/simple/simple.go).
This example does not include error handling.

```go
// Connect to socket
endPoint, _ := url.Parse("ws://localhost:4000/socket?foo=bar")
socket := phx.NewSocket(endPoint)
_ = socket.Connect()

// Join a channel
channel := socket.Channel("test:lobby", nil)
join, _ := channel.Join()
join.Receive("ok", func(response any) {
  log.Println("Joined channel:", channel.Topic(), response)
})

// Send an event
ping, _ := channel.Push("ping", "pong")
ping.Receive("ok", func(response any) {
  log.Println("Ping:", response)
})

// Listen for pushes or broadcasts from the server
channel.On("shout", func(payload any) {
  log.Println("received shout:", payload)
})
```

## CLI example

There is also a simple CLI example for interactively using the library to connect to any server/channel in
[examples/cli/cli.go](examples/cli/cli.go).

## Not implemented currently:

- Longpoll transport.
- Binary messages.
