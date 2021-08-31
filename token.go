// Those token source supports wild-scope installation token only currently.
package ghinstallation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/go-github/v38/github"
)

type TokenSource interface {
	Token(installationID int64) (*AccessToken, error)
}

type ReuseTokenSource struct {
	static *staticTokenSource
	source sync.Map
}

func NewReuseTokenSource(transport *AppsTransport) *ReuseTokenSource {
	return &ReuseTokenSource{
		static: NewStaticTokenSource(transport),
	}
}

func (t *ReuseTokenSource) Token(installationID int64) (*AccessToken, error) {
	raw, ok := t.source.Load(installationID)
	if ok {
		token := raw.(AccessToken)
		if !token.IsExpired() {
			// still available
			return &token, nil
		}
	}
	token, err := t.static.Token(installationID)
	if err != nil {
		return nil, err
	}
	t.source.Store(installationID, *token)

	return token, nil
}

type staticTokenSource struct {
	// not expose right now
	installationTokenOptions *github.InstallationTokenOptions // parameters restrict a token's access
	appsTransport            *AppsTransport
}

func NewStaticTokenSource(transport *AppsTransport) *staticTokenSource {
	return &staticTokenSource{
		appsTransport: transport,
	}
}

func (s *staticTokenSource) Token(installationID int64) (*AccessToken, error) {
	// Convert InstallationTokenOptions into a ReadWriter to pass as an argument to http.NewRequest.
	body, err := GetReadWriter(s.installationTokenOptions)
	if err != nil {
		return nil, fmt.Errorf("could not convert installation token parameters into json: %s", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/app/installations/%v/access_tokens", s.appsTransport.BaseURL, installationID), body)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %s", err)
	}

	// Set Content and Accept headers.
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", acceptHeader)

	resp, err := s.appsTransport.RoundTrip(req)
	e := &HTTPError{
		RootCause:      err,
		InstallationID: installationID,
		Response:       resp,
	}
	if err != nil {
		e.Message = fmt.Sprintf("could not get access_tokens from GitHub API for installation ID %v: %v", installationID, err)
		return nil, e
	}

	if resp.StatusCode/100 != 2 {
		e.Message = fmt.Sprintf("received non 2xx response status %q when fetching %v", resp.Status, req.URL)
		return nil, e
	}
	// Closing body late, to provide caller a chance to inspect body in an error / non-200 response status situation
	defer resp.Body.Close()

	var token AccessToken
	if err = json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	return &token, nil
}
