package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestFiveMHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/players.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"name":"cypek","ping":40},{"id":2,"name":"steve","ping":60}]`))
	})
	mux.HandleFunc("/info.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"vars":{"sv_maxClients":"48","sv_projectName":"x"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	p, err := Query(context.Background(), Spec{Kind: "fivem-http", Host: u.Hostname(), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if p.Online != 2 || p.Max != 48 || len(p.Names) != 2 || p.Names[1] != "steve" {
		t.Errorf("players = %+v", p)
	}
}
