package oceaauth

import (
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"net/http"
	"net/url"
	"strings"
)

var (
	errNoRefreshToken = fmt.Errorf("no refresh token")
)

func (o *TokenProvider) refreshToken() error {
	var err error

	if o.tokens.RefreshToken == "" {
		return errNoRefreshToken
	}

	o.tokens, err = o.exchangeRefreshToken(o.tokens.RefreshToken)
	if err != nil {
		return fmt.Errorf("failed to exchange token: %w", err)
	}

	zap.L().Info("auth: got token from refresh")
	return nil
}

func (o *TokenProvider) exchangeRefreshToken(refreshToken string) (tokens, error) {
	form := url.Values{}

	form.Add("client_id", OCEALoginClientID)
	form.Add("scope", OCEALoginScope)
	form.Add("grant_type", "refresh_token")
	form.Add("client_info", "1")
	form.Add("client-request-id", uuid.NewString())
	form.Add("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", OCEATokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens{}, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Origin", OCEAPortalHome)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", OCEAPortalHome)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := o.client.Do(req)
	if err != nil {
		return tokens{}, fmt.Errorf("failed exchange refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return tokens{}, fmt.Errorf("failed exchange refresh token: bad status code (expected 200): %d", resp.StatusCode)
	}

	return extractTokens(resp)
}
