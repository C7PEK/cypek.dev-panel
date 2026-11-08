// Package shim is the per-server supervisor process: it runs the game in a
// PTY, exposes a unix socket the panel attaches to, keeps a console backlog
// and writes console logs. `homepanel shim <id>` is what systemd starts.
package shim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// State of the supervised process as reported by the shim.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

// Frame types. The protocol is newline-delimited JSON in both directions.
const (
	// shim -> client
	TypeHello  = "hello"  // first frame after connect: state, pid, startedAt
	TypeOutput = "output" // console output chunk (raw, may contain \r\n and ANSI)
	TypeState  = "state"  // state change; exitCode set when stopped
	TypeError  = "error"  // non-fatal problem, e.g. input rejected
	// client -> shim
	TypeInput  = "input"  // write data to the process stdin (PTY)
	TypeStop   = "stop"   // graceful stop (stop command, then timeout, then kill)
	TypeKill   = "kill"   // SIGKILL immediately
	TypeResize = "resize" // PTY size
	TypePing   = "ping"   // liveness; answered with pong
	TypePong   = "pong"
)

// Frame is one protocol message.
type Frame struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	State     State  `json:"state,omitempty"`
	PID       int    `json:"pid,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
	Message   string `json:"message,omitempty"`
}

// MaxFrameBytes bounds a single frame on the wire (input lines are far smaller).
const MaxFrameBytes = 1 << 20

// MaxInputBytes bounds one input frame's payload.
const MaxInputBytes = 4096

// WriteFrame encodes f as one JSON line.
func WriteFrame(w io.Writer, f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// FrameReader decodes newline-delimited frames.
type FrameReader struct {
	sc *bufio.Scanner
}

// NewFrameReader wraps r.
func NewFrameReader(r io.Reader) *FrameReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), MaxFrameBytes)
	return &FrameReader{sc: sc}
}

// Next returns the next frame, or io.EOF.
func (fr *FrameReader) Next() (Frame, error) {
	for fr.sc.Scan() {
		line := fr.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var f Frame
		if err := json.Unmarshal(line, &f); err != nil {
			return Frame{}, fmt.Errorf("decode frame: %w", err)
		}
		return f, nil
	}
	if err := fr.sc.Err(); err != nil {
		return Frame{}, err
	}
	return Frame{}, io.EOF
}
