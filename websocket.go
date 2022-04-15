package phx

import (
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type Websocket struct {
	dialer          *websocket.Dialer
	handler         TransportHandler
	conn            *websocket.Conn
	endPoint        string
	requestHeader   http.Header
	done            chan any
	reconnect       chan any
	send            chan Message
	connectionTries int
	mu              sync.RWMutex
}

const busyWait = 100 * time.Millisecond

func NewWebsocket(dialer *websocket.Dialer, handler TransportHandler) *Websocket {
	return &Websocket{
		dialer:  dialer,
		handler: handler,
	}
}

func (w *Websocket) Connect(endPoint url.URL, requestHeader http.Header) error {
	endPoint.Path = path.Join(endPoint.Path, "websocket")

	w.endPoint = endPoint.String()
	w.requestHeader = requestHeader
	w.connectionTries = 0

	w.done = make(chan any)
	w.reconnect = make(chan any)
	w.send = make(chan Message, 100)

	go w.connectionManager()
	go w.writer()
	go w.reader()
	go w.heartbeat()

	return nil
}

func (w *Websocket) Disconnect() {
	w.close()
}

func (w *Websocket) IsConnected() bool {
	return w.connIsReady()
}

func (w *Websocket) dial() error {
	conn, _, err := w.dialer.Dial(w.endPoint, w.requestHeader)
	if err != nil {
		return err
	}
	//w.socket.Logger.Debugf("Connected conn: %+v\n\n", conn)
	//w.socket.Logger.Debugf("Connected resp: %+v\n", resp)

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.handler.OnConnOpen()

	return nil
}

func (w *Websocket) close() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.conn != nil {
		// attempt to gracefully close the connection by sending a close websocket message
		err := w.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			fmt.Println("error writing close message", err)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}

	close(w.done)
	w.handler.OnConnClose()
}

func (w *Websocket) teardown() {
	fmt.Println("teardown")
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return
	}

	_ = w.conn.Close()
	w.conn = nil
}

func (w *Websocket) connIsReady() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.conn != nil
}

func (w *Websocket) writeToConn(msg *Message) error {
	if !w.connIsReady() {
		return errors.New("connection is not open")
	}

	return w.conn.WriteJSON(msg)
}

func (w *Websocket) readFromConn(msg *Message) error {
	if !w.connIsReady() {
		return errors.New("connection is not open")
	}

	return w.conn.ReadJSON(msg)
}

func (w *Websocket) connectionManager() {
	defer func() {
		fmt.Println("connectionManager stopped")
		w.teardown()
	}()

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			return
		default:
		}

		if !w.connIsReady() {
			err := w.dial()
			if err != nil {
				w.handler.OnConnError(err)
				delay := w.handler.ReconnectAfter(w.connectionTries)
				w.connectionTries++
				time.Sleep(delay)
				continue
			}
		}

		select {
		case <-w.done:
			return
		case <-w.reconnect:
			w.teardown()
			continue
		}

	}
}

func (w *Websocket) writer() {
	defer func() {
		fmt.Println("writer stopped")
	}()

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			return
		default:
		}

		if !w.connIsReady() {
			time.Sleep(busyWait)
			continue
		}

		fmt.Println("Ready to write to socket")

		select {
		case <-w.done:
			return
		case msg := <-w.send:
			if !w.connIsReady() {
				time.Sleep(busyWait)
				continue
			}

			//w.socket.Logger.Printf(LogDebug, "websocket", "Sending message: %+v", msg)
			err := w.writeToConn(&msg)
			if err != nil {
				w.handler.OnConnError(err)
				w.reconnect <- true
				time.Sleep(busyWait)
				continue
			}
		}
	}
}

func (w *Websocket) reader() {
	defer func() {
		fmt.Println("reader stopped")
	}()

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			return
		default:
		}

		var msg Message

		if !w.connIsReady() {
			time.Sleep(busyWait)
			continue
		}

		fmt.Println("Ready to read from socket")

		err := w.readFromConn(&msg)
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				//w.socket.Logger.Printf(LogWarning, "websocket", "Could not read from socket: %T: %e / %+v", err, err, err)
			}
			w.reconnect <- true
			time.Sleep(busyWait)
			continue
		}

		w.handler.OnConnMessage(msg)
	}
}

func (w *Websocket) heartbeat() {
	// TODO: move this to socket, and check if heartbeat was received!
	//ticker := time.NewTicker(w.socket.HeartbeatInterval)
	ticker := time.NewTicker(5 * time.Second)
	defer func() {
		ticker.Stop()
		fmt.Println("heartbeat stopped")
	}()

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			return
		default:
		}

		if !w.connIsReady() {
			time.Sleep(busyWait)
			continue
		}

		select {
		case <-w.done:
			return
		case <-ticker.C:
			if !w.connIsReady() {
				time.Sleep(busyWait)
				continue
			}
			//w.socket.Logger.Printf(LogDebug, "websocket", "Sending heartbeat")
			w.send <- Message{Topic: "phoenix", Event: "heartbeat", Payload: nil, Ref: -1}
		}
	}
}
