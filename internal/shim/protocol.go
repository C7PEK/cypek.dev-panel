package shim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

const (
	TypeHello  = "hello"
	TypeOutput = "output"
	TypeState  = "state"
	TypeError  = "error"

	TypeInput  = "input"
	TypeStop   = "stop"
	TypeKill   = "kill"
	TypeResize = "resize"
	TypePing   = "ping"
	TypePong   = "pong"
)

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

const MaxFrameBytes = 1 << 20

const MaxInputBytes = 4096

func WriteFrame(w io.Writer, f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

type FrameReader struct {
	sc *bufio.Scanner
}

func NewFrameReader(r io.Reader) *FrameReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), MaxFrameBytes)
	return &FrameReader{sc: sc}
}

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
