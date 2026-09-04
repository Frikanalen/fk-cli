// Package fk is a client for the Frikanalen Django API. Request and
// response handling is generated from schema.yaml (see generate-client.sh);
// this package adds authentication and a small domain-shaped API on top.
package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	apiclient "github/frikanalen/fk-cli/fk-client/generated"
)

// Client talks to a Frikanalen API server, authenticating with a stored
// DRF auth token when one is available.
type Client struct {
	api        *apiclient.ClientWithResponses
	httpClient *http.Client
	// baseURL is the deployment the client talks to. The API lives under
	// /api on the same host as the website, so it also roots the page URLs
	// the CLI prints.
	baseURL string
	token   string
}

// Open builds a Client for the active environment, using the auth token
// stored for that environment.
func Open() (*Client, error) {
	return newClient(APIURL(), StoredToken())
}

// newClient builds a Client against baseURL, independent of viper, so
// tests can point it at an httptest.Server instead of real configuration.
func newClient(baseURL, token string) (*Client, error) {
	httpClient := &http.Client{}
	c := &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}

	api, err := apiclient.NewClientWithResponses(
		baseURL,
		apiclient.WithHTTPClient(httpClient),
		apiclient.WithRequestEditorFn(c.authenticate),
	)
	if err != nil {
		return nil, fmt.Errorf("building API client: %w", err)
	}
	c.api = api

	return c, nil
}

func (c *Client) authenticate(_ context.Context, req *http.Request) error {
	if c.token != "" {
		req.Header.Set("Authorization", "Token "+c.token)
	}
	return nil
}

// apiErrors is the envelope every non-2xx JSON response from the API uses.
type apiErrors struct {
	Errors []struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Attr   string `json:"attr"`
	} `json:"errors"`
}

func (e *apiErrors) Error() string {
	parts := make([]string, len(e.Errors))
	for i, item := range e.Errors {
		if item.Attr != "" {
			parts[i] = fmt.Sprintf("%s: %s", item.Attr, item.Detail)
		} else {
			parts[i] = item.Detail
		}
	}
	return strings.Join(parts, "; ")
}

// APIError is a non-2xx answer from the API. It keeps the status code apart
// from the message so callers can tell a fault worth retrying from a refusal
// that will be repeated just as firmly next time.
type APIError struct {
	Status int
	// Detail is the server's explanation: the joined contents of its
	// structured error envelope, or the raw body when it did not send one.
	Detail string
	// err is the structured envelope, when there was one, so errors.As can
	// still reach it.
	err error
}

func (e *APIError) Error() string { return fmt.Sprintf("%d: %s", e.Status, e.Detail) }
func (e *APIError) Unwrap() error { return e.err }

// checkResponse turns a non-2xx status into an error, preferring the API's
// structured {type, errors} envelope when the body has one.
func checkResponse(status int, body []byte) error {
	if status < 300 {
		return nil
	}
	var apiErr apiErrors
	if err := json.Unmarshal(body, &apiErr); err == nil && len(apiErr.Errors) > 0 {
		return &APIError{Status: status, Detail: apiErr.Error(), err: &apiErr}
	}
	return &APIError{Status: status, Detail: strings.TrimSpace(string(body))}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func simpleOrgs(orgs *[]apiclient.SimpleOrg) []SimpleOrg {
	if orgs == nil {
		return nil
	}
	out := make([]SimpleOrg, len(*orgs))
	for i, o := range *orgs {
		out[i] = SimpleOrg{Id: deref(o.Id), Name: o.Name}
	}
	return out
}

// Login exchanges an email and password for an auth token via
// POST /api/obtain-token and caches it on the client for subsequent
// requests. It does not persist the token anywhere; call Token after a
// successful Login and store it yourself if it should outlive the process.
func (c *Client) Login(ctx context.Context, email, password string) error {
	resp, err := c.api.ObtainTokenCreateWithResponse(ctx, apiclient.ObtainTokenCreateJSONRequestBody{
		Username: &email,
		Password: &password,
	})
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return err
	}

	c.token = deref(resp.JSON200.Token)
	return nil
}

// Token returns the auth token the client is currently sending, e.g. to
// persist it after a successful Login.
func (c *Client) Token() string {
	return c.token
}

// Profile fetches the authenticated user's own profile.
func (c *Client) Profile(ctx context.Context) (*User, error) {
	resp, err := c.api.UserRetrieveWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	u := resp.JSON200
	return &User{
		Id:        deref(u.Id),
		Email:     string(deref(u.Email)),
		IsStaff:   deref(u.IsStaff),
		EditorOf:  simpleOrgs(u.EditorOf),
		MemberOf:  simpleOrgs(u.MemberOf),
		FirstName: deref(u.FirstName),
		LastName:  deref(u.LastName),
	}, nil
}

// seriesPageSize is how many series ListSeries asks for per request.
// /api/series paginates with limit/offset, so a listing is a walk; a
// generous page keeps the number of round trips down.
const seriesPageSize = 100

// ListSeries returns the series belonging to an organization, walking the
// paginated endpoint until it has them all.
func (c *Client) ListSeries(ctx context.Context, orgId int) ([]Series, error) {
	var out []Series

	for offset := 0; ; offset += seriesPageSize {
		limit := seriesPageSize
		page := offset
		resp, err := c.api.SeriesListWithResponse(ctx, &apiclient.SeriesListParams{
			Limit:        &limit,
			Offset:       &page,
			Organization: &orgId,
		})
		if err != nil {
			return nil, fmt.Errorf("performing request: %w", err)
		}
		if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
			return nil, err
		}

		results := resp.JSON200.Results
		for _, s := range results {
			out = append(out, Series{Id: deref(s.Id), Name: s.Name})
		}

		// Stop on a short or empty page rather than trusting Count alone:
		// a server that reports a stale count would otherwise spin here.
		if len(results) < seriesPageSize || len(out) >= resp.JSON200.Count {
			return out, nil
		}
	}
}

// VideoURL returns the address of a video's page on the website of the
// deployment this client is talking to.
func (c *Client) VideoURL(videoId int) string {
	return fmt.Sprintf("%s/video/%d", c.baseURL, videoId)
}

// CreateVideo creates a new video record and returns its ID. It does not
// upload any file content; call UploadToken and Upload afterwards for that.
func (c *Client) CreateVideo(ctx context.Context, req CreateVideoRequest) (int, error) {
	body := apiclient.VideoCreateRequest{
		Name:         req.Title,
		Description:  &req.Description,
		Categories:   req.Categories,
		Organization: req.OrgId,
		SeriesId:     req.SeriesId,
	}

	resp, err := c.api.VideosCreateWithResponse(ctx, body)
	if err != nil {
		return 0, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return 0, err
	}

	return deref(resp.JSON201.Id), nil
}

// UploadToken fetches the capability that authorizes uploading a file for
// the given video.
func (c *Client) UploadToken(ctx context.Context, videoId int) (*VideoUploadToken, error) {
	resp, err := c.api.VideosUploadTokenRetrieveWithResponse(ctx, videoId)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	return &VideoUploadToken{
		UploadToken: deref(resp.JSON200.UploadToken),
		UploadURL:   deref(resp.JSON200.UploadUrl),
	}, nil
}

// IngestStatus reports how far the ingest pipeline has got with a video's
// uploaded file.
func (c *Client) IngestStatus(ctx context.Context, videoId int) (*IngestJob, error) {
	resp, err := c.api.VideosIngestRetrieveWithResponse(ctx, videoId)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	return &IngestJob{
		State:          string(resp.JSON200.State),
		Kind:           string(deref(resp.JSON200.Kind)),
		PercentageDone: resp.JSON200.PercentageDone,
		ErrorCode:      deref(resp.JSON200.ErrorCode),
	}, nil
}

// IngestFormats reads the desired variants from the ingest deployment that is
// currently answering for this environment. The endpoint is deliberately
// unauthenticated: the API token remains scoped to django-api.
func (c *Client) IngestFormats(ctx context.Context) (*DesiredFormats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ingest-api/formats", nil)
	if err != nil {
		return nil, fmt.Errorf("building ingest formats request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading ingest formats: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ingest formats response: %w", err)
	}
	if err := checkResponse(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var desired DesiredFormats
	if err := json.Unmarshal(body, &desired); err != nil {
		return nil, fmt.Errorf("decoding ingest formats: %w", err)
	}
	if len(desired.Formats) == 0 {
		return nil, fmt.Errorf("ingest formats response contains no desired formats")
	}
	for variant, revision := range desired.Formats {
		if variant == "" || revision < 1 {
			return nil, fmt.Errorf("ingest formats response contains invalid format %q at revision %d", variant, revision)
		}
	}
	return &desired, nil
}

// cataloguePageSize keeps a full catalogue to a handful of requests without
// asking django-api to return the entire database in one response.
const cataloguePageSize = 500

// ReadCatalogue returns every video, including unfinished ingests, with every
// videofile registered against it. A short paginated read is an error: acting
// on a partial catalogue would look exactly like a successful complete sweep.
func (c *Client) ReadCatalogue(ctx context.Context) (Catalogue, error) {
	videos := Catalogue{}
	for _, pass := range []struct {
		properImport bool
		name         string
	}{
		{properImport: false, name: "unfinished videos"},
		{properImport: true, name: "finished videos"},
	} {
		if err := c.readVideoPages(ctx, videos, pass.properImport, pass.name); err != nil {
			return nil, err
		}
	}

	if err := c.readVideofilePages(ctx, videos); err != nil {
		return nil, err
	}
	return videos, nil
}

func (c *Client) readVideoPages(ctx context.Context, videos Catalogue, properImport bool, name string) error {
	offset := 0
	expected := -1
	seen := 0

	for {
		limit := cataloguePageSize
		ordering := "id"
		resp, err := c.api.VideosListWithResponse(ctx, &apiclient.VideosListParams{
			Limit:        &limit,
			Offset:       &offset,
			Ordering:     &ordering,
			ProperImport: &properImport,
		})
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("reading %s: API returned no catalogue page", name)
		}
		page := resp.JSON200
		if expected < 0 {
			expected = page.Count
		}

		for _, row := range page.Results {
			id := deref(row.Id)
			videos[id] = &CatalogueVideo{
				Id:        id,
				Duration:  deref(row.Duration),
				Framerate: deref(row.Framerate),
			}
			seen++
		}

		if len(page.Results) == 0 || seen >= expected {
			break
		}
		offset += len(page.Results)
	}

	if seen != expected {
		return fmt.Errorf("incomplete catalogue: %s reported %d rows but returned %d", name, expected, seen)
	}
	return nil
}

func (c *Client) readVideofilePages(ctx context.Context, videos Catalogue) error {
	offset := 0
	expected := -1
	seen := 0

	for {
		limit := cataloguePageSize
		ordering := "id"
		resp, err := c.api.VideofilesListWithResponse(ctx, &apiclient.VideofilesListParams{
			Limit:    &limit,
			Offset:   &offset,
			Ordering: &ordering,
		})
		if err != nil {
			return fmt.Errorf("reading videofiles: %w", err)
		}
		if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
			return fmt.Errorf("reading videofiles: %w", err)
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("reading videofiles: API returned no catalogue page")
		}
		page := resp.JSON200
		if expected < 0 {
			expected = page.Count
		}

		for _, row := range page.Results {
			if video := videos[row.Video]; video != nil {
				video.Files = append(video.Files, CatalogueFile{
					Id:              deref(row.Id),
					Variant:         string(row.Variant),
					ProfileRevision: deref(row.ProfileRevision),
					IntegratedLufs:  row.IntegratedLufs,
				})
			}
			seen++
		}

		if len(page.Results) == 0 || seen >= expected {
			break
		}
		offset += len(page.Results)
	}

	if seen != expected {
		return fmt.Errorf("incomplete catalogue: videofiles reported %d rows but returned %d", expected, seen)
	}
	return nil
}

// QueueBackfill replaces a video's ingest job with a pending, low-priority
// archive job. Callers must check IngestStatus first and leave active work and
// queued uploads alone.
func (c *Client) QueueBackfill(ctx context.Context, videoId, priority int) error {
	state := apiclient.IngestStateEnumPending
	kind := apiclient.Backfill
	resp, err := c.api.VideosIngestReportWithResponse(ctx, videoId, apiclient.IngestJobRequest{
		State:    state,
		Kind:     &kind,
		Priority: &priority,
	})
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	return checkResponse(resp.StatusCode(), resp.Body)
}
