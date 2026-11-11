package shim

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type Client struct {
	conn   net.Conn
	wmu    sync.Mutex
	frames chan Frame
	Hello  Frame
	err    error
	done   chan struct{}
}

var ErrNotRunning = errors.New("server is not running")

func Dial(ctx context.Context, socketPath string) (*Client, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, ErrNotRunning
	}
	c := &Client{conn: conn, frames: make(chan Frame, 256), done: make(chan struct{})}
	fr := NewFrameReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	hello, err := fr.Next()
	if err != nil || hello.Type != TypeHello {
		conn.Close()
		return nil, fmt.Errorf("shim handshake on %s failed: %w", socketPath, err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	c.Hello = hello
	go c.readLoop(fr)
	return c, nil
}

func (c *Client) readLoop(fr *FrameReader) {
	defer close(c.frames)
	defer close(c.done)
	for {
		f, err := fr.Next()
		if err != nil {
			c.err = err
			return
		}
		select {
		case c.frames <- f:
		default:
			if f.Type != TypeOutput {
				c.frames <- f
			}
		}
	}
}

func (c *Client) Frames() <-chan Frame { return c.frames }

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	if errors.Is(c.err, net.ErrClosed) {
		return nil
	}
	return c.err
}

func (c *Client) Send(f Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return WriteFrame(c.conn, f)
}

func (c *Client) Input(data string) error { return c.Send(Frame{Type: TypeInput, Data: data}) }

func (c *Client) Close() error { return c.conn.Close() }

func Probe(ctx context.Context, socketPath string) (Frame, error) {
	c, err := Dial(ctx, socketPath)
	if err != nil {
		return Frame{}, err
	}
	defer c.Close()
	return c.Hello, nil
}
