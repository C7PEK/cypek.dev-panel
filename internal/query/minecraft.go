// Package query asks game servers how many players are online. Minecraft
// uses the Server List Ping protocol on the game port; nothing extra has to
// be enabled on the server. FiveM (info.json/players.json) comes later.
package query

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Players is the answer every query kind maps to.
type Players struct {
	Online int       `json:"online"`
	Max    int       `json:"max"`
	Names  []string  `json:"names,omitempty"` // best effort; Minecraft samples up to 12
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
}

// Spec says how to query one server.
type Spec struct {
	Kind string // minecraft-ping
	Host string // 127.0.0.1 normally
	Port int
}

// ErrUnsupported is returned for unknown kinds.
var ErrUnsupported = errors.New("unsupported query kind")

// Query runs one query with a short timeout.
func Query(ctx context.Context, spec Spec) (*Players, error) {
	switch spec.Kind {
	case "minecraft-ping":
		return minecraftPing(ctx, spec.Host, spec.Port)
	case "fivem-http":
		return fivemHTTP(ctx, spec.Host, spec.Port)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, spec.Kind)
	}
}

// Cache answers repeated status polls without hammering the game server.
type Cache struct {
	TTL     time.Duration
	mu      sync.Mutex
	entries map[string]cached
}

type cached struct {
	p   *Players
	err error
	at  time.Time
}

// NewCache returns a cache with the given freshness window.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{TTL: ttl, entries: map[string]cached{}}
}

// Get returns a fresh or cached answer for key (normally the server id).
func (c *Cache) Get(ctx context.Context, key string, spec Spec) (*Players, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Since(e.at) < c.TTL {
		c.mu.Unlock()
		return e.p, e.err
	}
	c.mu.Unlock()
	p, err := Query(ctx, spec)
	c.mu.Lock()
	c.entries[key] = cached{p: p, err: err, at: time.Now()}
	c.mu.Unlock()
	return p, err
}

// Forget drops a cached answer (after stop/start).
func (c *Cache) Forget(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// minecraftPing implements the modern (1.7+) status handshake:
// handshake(protocol -1, host, port, next=1) + status request, then a JSON
// status response. See https://minecraft.wiki/w/Java_Edition_protocol/Server_List_Ping
func minecraftPing(ctx context.Context, host string, port int) (*Players, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	var hs bytes.Buffer
	writeVarInt(&hs, 0x00) // packet id: handshake
	writeVarInt(&hs, -1)   // protocol version: -1 = "just asking"
	writeString(&hs, host)
	_ = binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1) // next state: status
	if err := writePacket(conn, hs.Bytes()); err != nil {
		return nil, err
	}
	if err := writePacket(conn, []byte{0x00}); err != nil { // status request
		return nil, err
	}
	payload, err := readPacket(conn)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(payload)
	if id, err := readVarInt(r); err != nil || id != 0x00 {
		return nil, fmt.Errorf("unexpected status packet id %d", id)
	}
	jsonLen, err := readVarInt(r)
	if err != nil || jsonLen < 0 || int(jsonLen) > r.Len() {
		return nil, errors.New("malformed status response")
	}
	var status struct {
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
			Sample []struct {
				Name string `json:"name"`
			} `json:"sample"`
		} `json:"players"`
	}
	if err := json.NewDecoder(io.LimitReader(r, int64(jsonLen))).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	p := &Players{Online: status.Players.Online, Max: status.Players.Max, Kind: "minecraft-ping", At: time.Now()}
	for _, s := range status.Players.Sample {
		if s.Name != "" {
			p.Names = append(p.Names, s.Name)
		}
	}
	return p, nil
}

const maxPacket = 1 << 20

func writeVarInt(w *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		b := byte(u & 0x7F)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		w.WriteByte(b)
		if u == 0 {
			return
		}
	}
}

func writeString(w *bytes.Buffer, s string) {
	writeVarInt(w, int32(len(s)))
	w.WriteString(s)
}

func writePacket(conn net.Conn, payload []byte) error {
	var out bytes.Buffer
	writeVarInt(&out, int32(len(payload)))
	out.Write(payload)
	_, err := conn.Write(out.Bytes())
	return err
}

func readVarInt(r io.ByteReader) (int32, error) {
	var result uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), nil
		}
	}
	return 0, errors.New("varint too long")
}

func readPacket(conn net.Conn) ([]byte, error) {
	br := &byteReader{r: conn}
	n, err := readVarInt(br)
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > maxPacket {
		return nil, fmt.Errorf("bad packet length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type byteReader struct{ r io.Reader }

func (b *byteReader) ReadByte() (byte, error) {
	var one [1]byte
	_, err := io.ReadFull(b.r, one[:])
	return one[0], err
}
