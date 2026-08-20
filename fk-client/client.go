// Package fk is a client for the Frikanalen Django API. Request and
// response handling is generated from schema.yaml (see generate-client.sh);
// this package adds authentication and a small domain-shaped API on top.
package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	apiclient "github/frikanalen/fk-cli/fk-client/generated"
)

// Client talks to a Frikanalen API server, authenticating with a stored
// DRF auth token when one is available.
type Client struct {
	api   *apiclient.ClientWithResponses
	token string
}

// Open builds a Client from the "API" and "token" configuration keys.
func Open() (*Client, error) {
	return newClient(viper.GetString("API"), viper.GetString("token"))
}

// newClient builds a Client against baseURL, independent of viper, so
// tests can point it at an httptest.Server instead of real configuration.
func newClient(baseURL, token string) (*Client, error) {
	c := &Client{token: token}

	api, err := apiclient.NewClientWithResponses(baseURL, apiclient.WithRequestEditorFn(c.authenticate))
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

// checkResponse turns a non-2xx status into an error, preferring the API's
// structured {type, errors} envelope when the body has one.
func checkResponse(status int, body []byte) error {
	if status < 300 {
		return nil
	}
	var apiErr apiErrors
	if err := json.Unmarshal(body, &apiErr); err == nil && len(apiErr.Errors) > 0 {
		return fmt.Errorf("%d: %w", status, &apiErr)
	}
	return fmt.Errorf("%d: %s", status, strings.TrimSpace(string(body)))
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

// CreateVideo creates a new video record and returns its ID. It does not
// upload any file content; call UploadToken and Upload afterwards for that.
func (c *Client) CreateVideo(ctx context.Context, req CreateVideoRequest) (int, error) {
	body := apiclient.VideoCreateRequest{
		Name:         req.Title,
		Description:  &req.Description,
		Categories:   req.Categories,
		Organization: req.OrgId,
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
	resp, err := c.api.VideosUploadTokenRetrieveWithResponse(ctx, strconv.Itoa(videoId))
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
	resp, err := c.api.VideosIngestRetrieveWithResponse(ctx, strconv.Itoa(videoId))
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	return &IngestJob{
		State:          string(resp.JSON200.State),
		PercentageDone: resp.JSON200.PercentageDone,
		ErrorCode:      deref(resp.JSON200.ErrorCode),
	}, nil
}
