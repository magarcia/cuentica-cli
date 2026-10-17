package testutil

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
)

type RoundTripCloser interface {
	http.RoundTripper
	io.Closer
}

// Transport exercises real HTTP serialization over net.Pipe without binding a
// socket. Close the returned transport after use.
func Transport(handler http.Handler) RoundTripCloser {
	listener := &pipeListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	server := &http.Server{Handler: handler}
	transport := &handlerTransport{server: server, done: make(chan struct{})}
	transport.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		client, remote := net.Pipe()
		select {
		case listener.connections <- remote:
			return client, nil
		case <-ctx.Done():
			_ = client.Close()
			_ = remote.Close()
			return nil, ctx.Err()
		case <-listener.closed:
			_ = client.Close()
			_ = remote.Close()
			return nil, net.ErrClosed
		}
	}}
	go func() {
		defer close(transport.done)
		_ = server.Serve(listener)
	}()
	return transport
}

type handlerTransport struct {
	*http.Transport
	server *http.Server
	done   chan struct{}
}

func (t *handlerTransport) Close() error {
	t.CloseIdleConnections()
	err := t.server.Close()
	<-t.done
	return err
}

type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}
}
