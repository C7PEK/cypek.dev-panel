package servers

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := db.Migrate(context.Background(), sqlDB, "", log); err != nil {
		t.Fatal(err)
	}
	return NewStore(sqlDB, filepath.Join(dir, "servers"))
}

func TestCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, &Server{Name: "", Run: shim.RunConfig{Command: "x"}}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: %v", err)
	}
	if _, err := s.Create(ctx, &Server{Name: "ok"}); err == nil {
		t.Error("missing command accepted")
	}
	if _, err := s.Create(ctx, &Server{Name: "ok", Category: "bogus", Run: shim.RunConfig{Command: "x"}}); err == nil {
		t.Error("bad category accepted")
	}

	srv, err := s.Create(ctx, &Server{Name: "  My Server ", Run: shim.RunConfig{Command: "/bin/true", Args: []string{"a"}, Stop: shim.StopConfig{Command: "stop"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(srv.ID) || srv.Name != "My Server" || srv.Category != CategoryOther || srv.TemplateID != "custom" {
		t.Errorf("created = %+v", srv)
	}
	if srv.Run.Stop.TimeoutSec != shim.DefaultStopTimeoutSec {
		t.Errorf("defaults not applied: %+v", srv.Run)
	}
	cfg, err := shim.LoadRunConfig(s.Dir(srv.ID))
	if err != nil || cfg.Command != "/bin/true" {
		t.Fatalf("run.json = %+v, %v", cfg, err)
	}

	got, err := s.Get(ctx, srv.ID)
	if err != nil || got.Name != "My Server" || got.Run.Args[0] != "a" || got.Run.Stop.Command != "stop" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := s.Get(ctx, "../etc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
	if _, err := s.Get(ctx, "zzzzzzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: %v", err)
	}

	srv2, _ := s.Create(ctx, &Server{Name: "another", Run: shim.RunConfig{Command: "y"}})
	list, err := s.List(ctx, nil)
	if err != nil || len(list) != 2 || list[0].Name != "another" {
		t.Fatalf("List = %v, %v", list, err)
	}
	list, _ = s.List(ctx, []string{srv2.ID})
	if len(list) != 1 || list[0].ID != srv2.ID {
		t.Errorf("filtered List = %v", list)
	}
	list, _ = s.List(ctx, []string{})
	if len(list) != 0 {
		t.Errorf("empty filter List = %v", list)
	}

	got.Name = "Renamed"
	got.Run.Command = "/bin/false"
	got.Autostart = true
	got.Limits.MemoryMB = 2048
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(ctx, srv.ID)
	if again.Name != "Renamed" || again.Run.Command != "/bin/false" || !again.Autostart || again.Limits.MemoryMB != 2048 {
		t.Errorf("after update = %+v", again)
	}
	cfg, _ = shim.LoadRunConfig(s.Dir(srv.ID))
	if cfg.Command != "/bin/false" {
		t.Error("run.json not rewritten on update")
	}

	if err := s.Delete(ctx, srv.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir(srv.ID)); !os.IsNotExist(err) {
		t.Error("server dir not removed")
	}
	if err := s.Delete(ctx, srv.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
