package fk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"

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

func (s *FrikanalenSession) getClient() *http.Client {
	if s.client == nil {
		jar, _ := cookiejar.New(nil)
		sessionCookie := &http.Cookie{
			Name:     "fk-session",
			Value:    s.sessionID,
			HttpOnly: true,
		}

		jar.SetCookies(s.apiURL, []*http.Cookie{sessionCookie})
		client := &http.Client{Jar: jar, Transport: &transport{underlyingTransport: http.DefaultTransport}}
		s.client = client
		return client
	} else {
		return s.client
	}
}

func Open() (*FrikanalenSession, error) {
	s := FrikanalenSession{}

	apiURL, err := url.Parse(viper.GetString("API"))
	if err != nil {
		return nil, err
	}

	s.apiURL = apiURL
	if viper.IsSet("sessionID") {
		s.sessionID = viper.GetString("sessionID")
	}

	return &s, nil
}

func (s *FrikanalenSession) Login(email string, password string) (string, error) {
	s.client = s.getClient()

	type loginrequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	body, err := json.Marshal(loginrequest{
		Email:    email,
		Password: password,
	})

	if err != nil {
		return "", err
	}

	loginEndpoint, _ := url.Parse("/auth/login")

	resp, err := s.client.Post(
		s.apiURL.ResolveReference(loginEndpoint).String(),
		"application/json",
		bytes.NewBuffer(body),
	)

	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		text, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		return "", fmt.Errorf("login returned %s, %s", resp.Status, text)

	}

	for _, v := range resp.Cookies() {
		if v.Name == "fk-session" {
			s.sessionID = v.Value
			return v.Value, nil
		}
	}

	return "", fmt.Errorf("did not get fk-session cookie")

}

func (s *FrikanalenSession) Upload(filespec string) (*UploadResponse, error) {
	f, err := os.Open(filespec)

	if err != nil {
		panic(err)
	}

	defer f.Close()

	config := tus.DefaultConfig()
	config.HttpClient = s.getClient()

	endpoint := s.apiURL.String() + "/upload/video"
	// create the tus client.
	client, err := tus.NewClient(endpoint, config)
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
