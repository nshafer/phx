package main

import (
	"github.com/nshafer/phx"
	"log"
	"net/url"
)

// This example works with a simple channel generated with:
//
//     mix phx.gen.channel Test
//
// then adding it to 'user_socket.ex' like:
//
//     channel "test:lobby", TruestackWeb.TestChannel
//
// See https://hexdocs.pm/phoenix/channels.html for more information

func main() {
	// Create a url.URL to connect to. `ws://` is non-encrypted websocket.
	endPoint, _ := url.Parse("ws://localhost:4000/socket?foo=bar")
	log.Println("Connecting to", endPoint)

	// Create a new phx.Socket
	socket := phx.NewSocket(endPoint)

	// Replace the default phx.NoopLogger with phx.SimpleLogger that prints messages
	// that are Info, Warning or Error. Set this to `phx.LogDebug` to see more info, such as the
	// raw messages being sent and received.
	socket.Logger = phx.NewSimpleLogger(phx.LogInfo)

	// Wait for the socket to connect before continuing. If it's not able to, it will keep
	// retrying forever.
	cont := make(chan bool)
	socket.OnOpen(func() {
		cont <- true
	})

	// Tell the socket to connect (or start retrying until it can connect)
	err := socket.Connect()
	if err != nil {
		log.Fatal(err)
	}

	// Wait for the connection
	<-cont

	// Create a phx.Channel to connect to the default 'room:lobby' channel with no params
	// (alternatively params could be a `map[string]string`)
	channel := socket.Channel("test:lobby", nil)

	// Join the channel. A phx.Push is returned which can be used to bind to replies and errors
	join, err := channel.Join()
	if err != nil {
		log.Fatal(err)
	}

	// Listen for a response and only continue once we know we're joined
	join.Receive("ok", func(response any) {
		log.Println("Joined channel:", channel.Topic(), response)
		cont <- true
	})
	join.Receive("error", func(response any) {
		log.Println("Join error", response)
	})

	// wait to be joined
	<-cont

	// Send a ping and see if we get a response
	ping, err := channel.Push("ping", "pong")
	if err != nil {
		log.Fatal(err)
	}

	// If we get the response we expect, then continue on
	ping.Receive("ok", func(response any) {
		ret, ok := response.(string)
		if ok && ret == "pong" {
			log.Println("Got pong from our ping")
			cont <- true
		}
	})

	// If there is an error or timeout, then bail
	ping.Receive("error", func(response any) {
		log.Fatal(response)
	})
	ping.Receive("timeout", func(response any) {
		log.Fatal(response)
	})

	// Wait for ping response
	<-cont

	// Let's listen for the "shout" broadcasts
	channel.On("shout", func(payload any) {
		log.Println("received shout:", payload)
	})

	// Let's send a shout, which we will also receive. The default `handle_in("shout", ...)` just
	// rebroadcasts what we send, and `broadcast/3` requires the payload to be a map, so we need
	// to shout a map. Your custom `handle_in/3` functions could of course handle whatever payload
	// you want to send them.
	shout, err := channel.Push("shout", map[string]string{"hello": "from go"})
	if err != nil {
		log.Fatal(err)
	}

	// The default `handle_in("shout", ...)` does not send a reply! So it will eventually timeout, which
	// can be caught with the "timeout" event.
	shout.Receive("timeout", func(response any) {
		log.Println("shout timeout", response)
	})

	// Now we will block forever, hit ctrl+c to exit
	select {}
}
