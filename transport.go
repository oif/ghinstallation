package ghinstallation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/google/go-github/v38/github"
)

const (
	acceptHeader = "application/vnd.github.v3+json"
	apiBaseURL   = "https://api.github.com"
)

// Transport provides a http.RoundTripper by wrapping an existing
// http.RoundTripper and provides GitHub Apps authentication as an
// installation.
//
// Client can also be overwritten, and is useful to change to one which
// provides retry logic if you do experience retryable errors.
//
// See https://developer.github.com/apps/building-integrations/setting-up-and-registering-github-apps/about-authentication-options-for-github-apps/
type Transport struct {
	BaseURL                  string                           // BaseURL is the scheme and host for GitHub API, defaults to https://api.github.com
	Client                   Client                           // Client to use to refresh tokens, defaults to http.Client with provided transport
	tr                       http.RoundTripper                // tr is the underlying roundtripper being wrapped
	appID                    int64                            // appID is the GitHub App's ID
	installationID           int64                            // installationID is the GitHub App Installation ID
	InstallationTokenOptions *github.InstallationTokenOptions // parameters restrict a token's access

	tokenSource TokenSource
}

// accessToken is an installation access token response from GitHub
type AccessToken struct {
	Token        string                         `json:"token"`
	ExpiresAt    time.Time                      `json:"expires_at"`
	Permissions  github.InstallationPermissions `json:"permissions,omitempty"`
	Repositories []github.Repository            `json:"repositories,omitempty"`
}

func (t AccessToken) IsExpired() bool {
	return t.ExpiresAt.Add(-time.Minute).Before(time.Now())
}

// HTTPError represents a custom error for failing HTTP operations.
// Example in our usecase: refresh access token operation.
// It enables the caller to inspect the root cause and response.
type HTTPError struct {
	Message        string
	RootCause      error
	InstallationID int64
	Response       *http.Response
}

func (e *HTTPError) Error() string {
	return e.Message
}

var _ http.RoundTripper = &Transport{}

// NewKeyFromFile returns a Transport using a private key from file.
func NewKeyFromFile(tr http.RoundTripper, appID, installationID int64, privateKeyFile string, tokenSource TokenSource) (*Transport, error) {
	privateKey, err := ioutil.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %s", err)
	}
	return New(tr, appID, installationID, privateKey, tokenSource)
}

// Client is a HTTP client which sends a http.Request and returns a http.Response
// or an error.
type Client interface {
	Do(*http.Request) (*http.Response, error)
}

// New returns an Transport using private key. The key is parsed
// and if any errors occur the error is non-nil.
//
// The provided tr http.RoundTripper should be shared between multiple
// installations to ensure reuse of underlying TCP connections.
//
// The returned Transport's RoundTrip method is safe to be used concurrently.
func New(tr http.RoundTripper, appID, installationID int64, privateKey []byte, tokenSource TokenSource) (*Transport, error) {
	atr, err := NewAppsTransport(tr, appID, privateKey)
	if err != nil {
		return nil, err
	}

	return NewFromAppsTransport(atr, installationID, tokenSource), nil
}

// NewFromAppsTransport returns a Transport using an existing *AppsTransport.
func NewFromAppsTransport(atr *AppsTransport, installationID int64, tokenSource TokenSource) *Transport {
	return &Transport{
		BaseURL:        atr.BaseURL,
		Client:         &http.Client{Transport: atr.tr},
		tr:             atr.tr,
		appID:          atr.appID,
		installationID: installationID,
		tokenSource:    tokenSource,
	}
}

// RoundTrip implements http.RoundTripper interface.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.Token()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+token.Token)
	req.Header.Add("Accept", acceptHeader) // We add to "Accept" header to avoid overwriting existing req headers.
	resp, err := t.tr.RoundTrip(req)
	return resp, err
}

// Token checks the active token expiration and renews if necessary. Token returns
// a valid access token. If renewal fails an error is returned.
func (t *Transport) Token() (*AccessToken, error) {
	return t.tokenSource.Token(t.installationID)
}

// GetReadWriter converts a body interface into an io.ReadWriter object.
func GetReadWriter(i interface{}) (io.ReadWriter, error) {
	var buf io.ReadWriter
	if i != nil {
		buf = new(bytes.Buffer)
		enc := json.NewEncoder(buf)
		err := enc.Encode(i)
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}
