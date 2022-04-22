package phx

import (
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"path"
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
	close           chan bool
	reconnect       chan bool
	closeMsg        chan bool
	send            chan Message
	connectionTries int
	mu              sync.RWMutex
	started         bool
	closing         bool
	reconnecting    bool
	waitingForClose bool
}

func NewWebsocket(dialer *websocket.Dialer, handler TransportHandler) *Websocket {
	return &Websocket{
		dialer:  dialer,
		handler: handler,
	}
}

func (w *Websocket) Connect(endPoint url.URL, requestHeader http.Header) error {
	if w.isStarted() {
		return errors.New("connect was already called")
	}

	w.startup(endPoint, requestHeader)
	return nil
}

func (w *Websocket) Disconnect() error {
	if !w.isStarted() {
		return errors.New("not connected")
	}

	if w.connIsSet() {
		w.sendClose()
	} else {
		w.teardown()
	}
	return nil
}

func (w *Websocket) Reconnect() error {
	if !w.isStarted() {
		return errors.New("not connected")
	}

	w.sendReconnect()
	return nil
}

func (w *Websocket) IsConnected() bool {
	return w.connIsReady()
}

func (w *Websocket) Send(msg Message) {
	w.send <- msg
}

func (w *Websocket) startup(endPoint url.URL, requestHeader http.Header) {
	endPoint.Path = path.Join(endPoint.Path, "websocket")

	w.endPoint = endPoint.String()
	w.requestHeader = requestHeader

	w.connectionTries = 0

	w.done = make(chan any)
	w.close = make(chan bool)
	w.closeMsg = make(chan bool)
	w.reconnect = make(chan bool)
	w.send = make(chan Message, messageQueueLength)

	w.setReconnecting(false)
	w.setClosing(false)

	go w.connectionManager()
	go w.connectionWriter()
	go w.connectionReader()

	w.setStarted(true)
}

func (w *Websocket) teardown() {
	//fmt.Println("teardown")

	// Tell the goroutines to exit
	close(w.done)
	close(w.close)
	close(w.closeMsg)
	close(w.reconnect)
	close(w.send)

	w.setStarted(false)
	w.setReconnecting(false)
	w.setClosing(false)
}

func (w *Websocket) dial() error {
	conn, _, err := w.dialer.Dial(w.endPoint, w.requestHeader)
	if err != nil {
		return err
	}
	//w.socket.Logger.Debugf("Connected conn: %+v\n\n", conn)
	//w.socket.Logger.Debugf("Connected resp: %+v\n", resp)

	w.setConn(conn)
	w.setReconnecting(false)
	w.handler.onConnOpen()

	return nil
}

func (w *Websocket) closeConn() {
	//fmt.Println("closeConn")

	if w.connIsSet() {
		// attempt to gracefully close the connection by sending a close websocket message
		err := w.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err == nil {
			// Wait for a close message to be received by `connectionReader`, or time out after 5 seconds
			w.setWaitingForClose(true)
			select {
			case <-w.closeMsg:
			case <-time.After(3 * time.Second):
			}
		}
	}

	if w.connIsSet() {
		err := w.conn.Close()
		if err != nil {
			w.handler.onConnError(err)
		}

		w.setConn(nil)
	}

	w.handler.onConnClose()
	w.setClosing(false)
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
	//fmt.Println("connectionManager started")
	//defer fmt.Println("connectionManager stopped")

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			return
		default:
		}

		if !w.isClosing() && !w.connIsSet() {
			err := w.dial()
			if err != nil {
				w.handler.onConnError(err)
				w.setReconnecting(true)
				delay := w.handler.reconnectAfter(w.connectionTries)
				w.connectionTries++
				select {
				case <-w.done:
				case <-time.After(delay):
				}
				continue
			}
		}

		select {
		case <-w.done:
			return
		case <-w.close:
			w.closeConn()
			w.teardown()
		case <-w.reconnect:
			w.closeConn()
		}
	}
}

func (w *Websocket) connectionWriter() {
	//fmt.Println("connectionWriter started")
	//defer fmt.Println("connectionWriter stopped")

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
		case msg := <-w.send:
			// If there is a message to send, but we're not connected, then wait until we are.
			if !w.connIsReady() {
				time.Sleep(busyWait)
				continue
			}

			// Send the message
			err := w.writeToConn(&msg)

			// If there were any errors sending, then tell the connectionManager to reconnect
			if err != nil {
				w.handler.onWriteError(err)
				w.sendReconnect()
				time.Sleep(busyWait)
				continue
			}
		}
	}
}

func (w *Websocket) connectionReader() {
	//fmt.Println("connectionReader started")
	//defer fmt.Println("connectionReader stopped")

	for {
		// Check if we have been told to finish
		select {
		case <-w.done:
			//fmt.Println("connectionReader stopping")
			return
		default:
		}

		var msg Message

		// Wait until we're connected
		if !w.connIsReady() {
			time.Sleep(busyWait)
			continue
		}

		// Read the next message from the websocket. This blocks until there is a message or error
		err := w.readFromConn(&msg)

		// If there were any errors, tell the connectionManager to reconnect
		if err != nil {
			//fmt.Printf("read error %e %v\n", err, err)
			if websocket.IsCloseError(err, 1000) {
				if w.isWaitingForClose() {
					// tell the connectionManager that we got the close message
					w.closeMsg <- true
				}
			} else {
				w.handler.onReadError(err)
				w.sendReconnect()
			}

			time.Sleep(busyWait)
			continue
		}

		w.handler.onConnMessage(msg)
	}
}

func (w *Websocket) setStarted(started bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.started = started
}

func (w *Websocket) isStarted() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.started
}

func (w *Websocket) setClosing(closing bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closing = closing
}

func (w *Websocket) isClosing() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.closing
}

func (w *Websocket) sendClose() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closing == true {
		return
	}

	w.closing = true
	w.close <- true
}

func (w *Websocket) setReconnecting(reconnecting bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.reconnecting = reconnecting
}

func (w *Websocket) isReconnecting() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.reconnecting
}

func (w *Websocket) sendReconnect() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.reconnecting || w.closing {
		return
	}

	w.reconnecting = true
	w.reconnect <- true
}

func (w *Websocket) setConn(conn *websocket.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.conn = conn
}

func (w *Websocket) connIsSet() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.conn != nil
}

func (w *Websocket) connIsReady() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.started && !w.closing && !w.reconnecting && w.conn != nil
}

func (w *Websocket) setWaitingForClose(waitingForClose bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.waitingForClose = waitingForClose
}

func (w *Websocket) isWaitingForClose() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.waitingForClose
}
