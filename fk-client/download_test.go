package fk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mediaServer serves a video record whose files point back at the same
// server, plus the files themselves.
func mediaServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/videos/7":
			writeJSON(w, 200, map[string]any{
				"id": 7, "name": "x",
				"files": map[string]any{
					"original": map[string]any{"url": srv.URL + "/media/7/original/talk.mov", "mimeType": nil},
					"webmMed":  map[string]any{"url": "/media/7/webm_med/talk.webm", "mimeType": "video/webm"},
				},
			})
		case "/media/7/original/talk.mov", "/media/7/webm_med/talk.webm":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("credentials sent to the media server")
			}
			_, _ = fmt.Fprint(w, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVideoFileFindsVariants(t *testing.T) {
	srv := mediaServer(t, "")
	c, err := newClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	link, err := c.VideoFile(context.Background(), 7, VariantOriginal)
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != srv.URL+"/media/7/original/talk.mov" {
		t.Errorf("original URL = %q", link.URL)
	}

	// camelCase key maps back to the snake_case variant name, and a
	// relative URL is resolved against the API host.
	link, err = c.VideoFile(context.Background(), 7, "webm_med")
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != srv.URL+"/media/7/webm_med/talk.webm" || link.MimeType != "video/webm" {
		t.Errorf("webm_med link = %+v", link)
	}

	_, err = c.VideoFile(context.Background(), 7, "broadcast")
	var missing *ErrNoSuchVariant
	if !errors.As(err, &missing) {
		t.Fatalf("expected ErrNoSuchVariant, got %v", err)
	}
	if got := strings.Join(missing.Available, ","); got != "original,webm_med" {
		t.Errorf("available = %q", got)
	}
}

func TestDownloadWritesFileAtomically(t *testing.T) {
	srv := mediaServer(t, "hello world")
	c, err := newClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "talk.mov")

	var last int64
	if err := c.Download(context.Background(), srv.URL+"/media/7/original/talk.mov", dest, func(n, _ int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "hello world" {
		t.Fatalf("file = %q, %v", got, err)
	}
	if last != int64(len("hello world")) {
		t.Errorf("last progress = %d", last)
	}

	// A failed download leaves nothing behind.
	if err := c.Download(context.Background(), srv.URL+"/media/nope", filepath.Join(dir, "nope"), nil); err == nil {
		t.Fatal("expected error for 404")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{"original": "original", "webmMed": "webm_med", "largeThumb": "large_thumb", "vc1": "vc1"} {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
