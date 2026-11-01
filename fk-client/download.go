package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// VariantOriginal is the file exactly as it was uploaded, before ingest
// derived anything from it.
const VariantOriginal = "original"

// VideoFileLink is where one registered file of a video can be fetched.
type VideoFileLink struct {
	URL      string
	MimeType string
}

// ErrNoSuchVariant reports that a video has no file of the requested
// variant, and which ones it does have.
type ErrNoSuchVariant struct {
	VideoId   int
	Variant   string
	Available []string
}

func (e *ErrNoSuchVariant) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("video %d has no files registered", e.VideoId)
	}
	return fmt.Sprintf("video %d has no %q file; available: %s",
		e.VideoId, e.Variant, strings.Join(e.Available, ", "))
}

// VideoFile looks up where a video's file of the given variant lives. The
// variant is spelt the way the API's videofile records spell it (original,
// broadcast, webm_med, ...).
func (c *Client) VideoFile(ctx context.Context, videoId int, variant string) (*VideoFileLink, error) {
	resp, err := c.api.VideosRetrieveWithResponse(ctx, videoId)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	// The files map is keyed by variant, but the API's camelCase renderer
	// rewrites the keys (webm_med becomes webmMed). Decoding the map rather
	// than the generated struct keeps variants the schema has not caught up
	// with downloadable.
	var video struct {
		Files map[string]struct {
			URL      string  `json:"url"`
			MimeType *string `json:"mimeType"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Body, &video); err != nil {
		return nil, fmt.Errorf("decoding video %d: %w", videoId, err)
	}

	available := make([]string, 0, len(video.Files))
	for key, link := range video.Files {
		name := snakeCase(key)
		if name == variant {
			resolved, err := c.resolve(link.URL)
			if err != nil {
				return nil, fmt.Errorf("video %d %s file: %w", videoId, variant, err)
			}
			return &VideoFileLink{URL: resolved, MimeType: deref(link.MimeType)}, nil
		}
		available = append(available, name)
	}
	sort.Strings(available)
	return nil, &ErrNoSuchVariant{VideoId: videoId, Variant: variant, Available: available}
}

// resolve makes a media URL absolute. The API normally hands out absolute
// URLs, but a deployment configured with a bare path prefix would not, and
// such a path belongs to the host the client is talking to.
func (c *Client) resolve(ref string) (string, error) {
	base, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("bad media URL %q: %w", ref, err)
	}
	return base.ResolveReference(u).String(), nil
}

// snakeCase undoes the API's camelCase key rendering: webmMed -> webm_med.
func snakeCase(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteByte('_')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Download fetches fileURL into dest. It writes to a temporary file next to
// dest and renames it into place only once the whole body has arrived, so an
// interrupted download never leaves something that looks complete.
// onProgress, when set, is called with the bytes received so far and the
// expected total, which is -1 when the server did not say.
//
// The media server is public, so no credentials are sent: the API token is
// scoped to the API and need not leave it.
func (c *Client) Download(ctx context.Context, fileURL, dest string, onProgress func(received, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("building download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", fileURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("downloading %s: %w", fileURL, checkResponse(resp.StatusCode, body))
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*.part")
	if err != nil {
		return err
	}
	// CreateTemp makes the file private; a download is an ordinary file.
	_ = tmp.Chmod(0o644)
	defer func() {
		// A no-op once the rename has happened.
		_ = os.Remove(tmp.Name())
	}()

	var src io.Reader = resp.Body
	if onProgress != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, onProgress: onProgress}
	}
	n, err := io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading %s: %w", fileURL, err)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fmt.Errorf("downloading %s: got %d of %d bytes", fileURL, n, resp.ContentLength)
	}
	return os.Rename(tmp.Name(), dest)
}

type progressReader struct {
	r          io.Reader
	received   int64
	total      int64
	onProgress func(received, total int64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	p.received += int64(n)
	p.onProgress(p.received, p.total)
	return n, err
}
