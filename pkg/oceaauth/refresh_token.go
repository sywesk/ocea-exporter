package oceaauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	errNoRefreshToken  = errors.New("no refresh token")
	errRefreshRejected = errors.New("refresh token rejected")
)

func (o *TokenProvider) refreshToken() error {
	if o.tokens.RefreshToken == "" {
		return errNoRefreshToken
	}

	next, err := o.exchangeRefreshToken(o.tokens.RefreshToken)
	if err != nil {
		return fmt.Errorf("failed to exchange token: %w", err)
	}

	if next.RefreshToken == "" {
		next.RefreshToken = o.tokens.RefreshToken
	}
	if next.RefreshTokenExpiresIn == 0 {
		next.RefreshTokenExpiresIn = o.tokens.RefreshTokenExpiresIn
		next.NotBefore = o.tokens.NotBefore
	}
	o.tokens = next
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

	req, err := o.newRequest("POST", OCEATokenEndpoint, strings.NewReader(form.Encode()))
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

	if resp.StatusCode == http.StatusBadRequest {
		var failure struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&failure) == nil && failure.Error == "invalid_grant" {
			return tokens{}, errRefreshRejected
		}
	}
	if resp.StatusCode != 200 {
		return tokens{}, fmt.Errorf("failed exchange refresh token: bad status code (expected 200): %d", resp.StatusCode)
	}

	return extractTokens(resp)
}
