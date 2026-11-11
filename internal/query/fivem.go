package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func fivemHTTP(ctx context.Context, host string, port int) (*Players, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	base := fmt.Sprintf("http://%s:%d", host, port)
	get := func(path string, into any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: HTTP %s", path, resp.Status)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(into)
	}
	var players []struct {
		Name string `json:"name"`
	}
	if err := get("/players.json", &players); err != nil {
		return nil, err
	}
	p := &Players{Online: len(players), Kind: "fivem-http", At: time.Now()}
	for _, pl := range players {
		if pl.Name != "" {
			p.Names = append(p.Names, pl.Name)
		}
	}
	var info struct {
		Vars map[string]any `json:"vars"`
	}
	if err := get("/info.json", &info); err == nil {
		switch v := info.Vars["sv_maxClients"].(type) {
		case string:
			p.Max, _ = strconv.Atoi(v)
		case float64:
			p.Max = int(v)
		}
	}
	return p, nil
}
