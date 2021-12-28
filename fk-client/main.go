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
	"github.com/spf13/viper"
	"github.com/toresbe/go-tus"
)

type FrikanalenSession struct {
	sessionID string
	apiURL    *url.URL
	client    *http.Client
}

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
			viper.WriteConfig()
			return nil
		}
	}

	return fmt.Errorf("did not get fk-session cookie")

}

/*
func (s *FrikanalenSession) CreateVideo(Organization int, Categories []int, Title string, Description string, MediaId int) (int, error) {
	s.client = s.getClient()

	type videorequest struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		MediaId     int    `json:"mediaId"`
		Categories  []int  `json:"categories"`
	}

	body, err := json.Marshal(videorequest{
		Title,
		Description,
		MediaId,
		Categories,
	})
	log.Println(string(body))
	if err != nil {
		return 0, err
	}

	videoCreatePath, _ := url.Parse(fmt.Sprintf("/organizations/%d/videos", Organization))

	resp, err := s.client.Post(
		s.apiURL.ResolveReference(videoCreatePath).String(),
		"application/json",
		bytes.NewBuffer(body),
	)

	if err != nil {
		return 0, err
	}

	text, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf(
			"could not create video, http %d: %s",
			resp.StatusCode,
			text,
		)
	}

	log.Println(string(text))
	return 1337, nil
}
*/

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

	config.HttpClient = getClient(apiURL, getSessionID())

	apiURL.Path = "/upload/video"

	// create the tus client.
	client, err := tus.NewClient(apiURL.String(), config)
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
	json.Unmarshal(client.Response, &response)

	return &response, nil
}

/*
func main() {
	session := FrikanalenSession{}

	err := session.Login("dev-admin@frikanalen.no", "dev-admin")
	if err != nil {
		log.Fatal(err)
	}

	upload, err := session.Upload()
	if err != nil {
		log.Fatal(err)
	}

	log.Println(upload)
}
*/
