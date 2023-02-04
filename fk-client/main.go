package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"

	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/toresbe/go-tus"
)

type UploadResponse struct {
	MediaId int    `json:"id"`
	JobId   string `json:"job"`
}

type transport struct {
	underlyingTransport http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.AddCookie(&http.Cookie{Name: "fk-csrf", Value: "my-secret-token"})
	req.Header.Add("x-csrf-token", "my-secret-token")
	return t.underlyingTransport.RoundTrip(req)
}

func getClient(apiURL *url.URL, sessionID *string) *http.Client {
	jar, _ := cookiejar.New(nil)

	if sessionID != nil {
		sessionCookie := &http.Cookie{
			Name:     "fk-session",
			Value:    *sessionID,
			HttpOnly: true,
		}
		jar.SetCookies(apiURL, []*http.Cookie{sessionCookie})
	}

	client := &http.Client{Jar: jar, Transport: &transport{underlyingTransport: http.DefaultTransport}}

	return client
}

func getSessionID() *string {
	if viper.IsSet("sessionID") {
		retval := new(string)
		*retval = viper.GetString("sessionID")
		return retval
	}

	return nil
}

func Open() (*Client, error) {
	apiURL, err := url.Parse(viper.GetString("API"))

	if err != nil {
		return nil, err
	}

	c := Client{
		Server: apiURL.String(),
		Client: getClient(apiURL, getSessionID()),
	}

	return &c, nil
}

func (c *Client) Login(Email openapi_types.Email, Password string) error {
	resp, err := c.LoginUser(context.Background(), LoginUserJSONRequestBody{
		Email,
		Password,
	})

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		text, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("login returned %s, %s", resp.Status, text)

	}

	for _, v := range resp.Cookies() {
		if v.Name == "fk-session" {
			viper.Set("sessionID", v.Value)
			err = viper.WriteConfig()
			if err != nil {
				log.Warnln("Could not write config file", viper.ConfigFileUsed())
			}
			return nil
		}
	}

	return nil
}

func (c *Client) Upload(filespec string) (*UploadResponse, error) {
	f, err := os.Open(filespec)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	config := tus.DefaultConfig()

	apiURL, err := url.Parse(viper.GetString("API"))
	if err != nil {
		return nil, err
	}

	uploadURL := apiURL
	uploadURL.Path = "/upload/video"

	config.HttpClient = getClient(uploadURL, getSessionID())

	// create the tus client.
	client, err := tus.NewClient(uploadURL.String(), config)
	if err != nil {
		return nil, err
	}

	// create an upload from a file.
	upload, err := tus.NewUploadFromFile(f)
	if err != nil {
		return nil, err
	}

	// create the uploader.
	uploader, err := client.CreateUpload(upload)
	if err != nil {
		return nil, err
	}

	// start the uploading process.
	err = uploader.Upload()
	if err != nil {
		return nil, err
	}

	response := UploadResponse{}

	err = json.Unmarshal(client.Response, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}
