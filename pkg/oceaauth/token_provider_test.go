package oceaauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type tokenTransport func(*http.Request) (*http.Response, error)

func (f tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCachedAccessTokenDoesNotCallNetwork(t *testing.T) {
	p := NewTokenProvider("user", "password")
	p.tokens = tokens{AccessToken: "cached", ExpiresOn: time.Now().Add(time.Hour).Unix()}
	p.client.Transport = tokenTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network call"); return nil, nil })
	if got, err := p.GetToken(); err != nil || got != "cached" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTokenRefresh(t *testing.T) {
	p := NewTokenProvider("user", "password")
	p.tokens = tokens{AccessToken: "expired", RefreshToken: "refresh", NotBefore: time.Now().Unix(), RefreshTokenExpiresIn: 3600}
	p.client.Transport = tokenTransport(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" {
			t.Fatalf("wrong refresh request: %v", r.Form)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"access_token":"new","refresh_token":"rotated","expires_on":%d}`, time.Now().Add(time.Hour).Unix()))), Header: make(http.Header)}, nil
	})
	if got, err := p.GetToken(); err != nil || got != "new" || p.tokens.RefreshToken != "rotated" {
		t.Fatalf("refresh failed: %q, %v", got, err)
	}
}

func TestTransientRefreshFailurePreservesTokens(t *testing.T) {
	p := NewTokenProvider("user", "password")
	p.tokens = tokens{AccessToken: "expired", RefreshToken: "refresh", NotBefore: time.Now().Unix(), RefreshTokenExpiresIn: 3600}
	p.client.Transport = tokenTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	if _, err := p.GetToken(); err == nil {
		t.Fatal("expected refresh failure")
	}
	if p.tokens.RefreshToken != "refresh" {
		t.Fatal("transient failure destroyed refresh token")
	}
}

func TestExtractTokensRejectsMissingAccessToken(t *testing.T) {
	for _, body := range []string{`{}`, `{"error":"invalid_grant"}`, `{"access_token":"","expires_on":100}`} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
		if _, err := extractTokens(resp); err == nil {
			t.Errorf("accepted invalid tokens: %s", body)
		}
	}
}

func TestCredentialsFlowAndRejectedRefreshRecovery(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		refreshRejected, badState bool
	}{{name: "fresh login"}, {name: "rejected refresh", refreshRejected: true}, {name: "mismatched OAuth state", badState: true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewTokenProvider("user@example.com", "secret")
			if tc.refreshRejected {
				p.tokens = tokens{AccessToken: "expired", RefreshToken: "revoked", NotBefore: time.Now().Unix(), RefreshTokenExpiresIn: 3600}
			}
			var verifier, challenge, requestState string
			calls := 0
			p.client.Transport = tokenTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				status, body := 200, ""
				headers := make(http.Header)
				switch {
				case strings.HasSuffix(r.URL.Path, "/authorize"):
					challenge = r.URL.Query().Get("code_challenge")
					requestState = r.URL.Query().Get("state")
					headers.Add("Set-Cookie", "x-ms-cpim-csrf=csrf123; Path=/; Secure")
					body = `var SETTINGS = {"transId":"StateProperties=abc","pageViewId":"abcd-1234","csrf":"csrf123"}`
				case strings.HasSuffix(r.URL.Path, "/SelfAsserted"):
					r.ParseForm()
					if r.Form.Get("email") != "user@example.com" || r.Form.Get("password") != "secret" || r.Header.Get("X-CSRF-TOKEN") != "csrf123" {
						t.Fatal("bad credentials request")
					}
					body = `{"status":"200"}`
				case strings.HasSuffix(r.URL.Path, "/confirmed"):
					status = 302
					state := requestState
					if tc.badState {
						state = "unexpected-state"
					}
					headers.Set("Location", OCEAPortalHome+"/#code=auth-code&state="+url.QueryEscape(state))
				case strings.HasSuffix(r.URL.Path, "/token"):
					r.ParseForm()
					if r.Form.Get("grant_type") == "refresh_token" {
						status = 400
						body = `{"error":"invalid_grant"}`
						break
					}
					if r.Form.Get("code") != "auth-code" {
						t.Fatal("bad auth code")
					}
					verifier = r.Form.Get("code_verifier")
					body = fmt.Sprintf(`{"access_token":"access","refresh_token":"refresh","expires_on":%d}`, time.Now().Add(time.Hour).Unix())
				default:
					t.Fatalf("unexpected URL: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: headers, Request: r}, nil
			})
			got, err := p.GetToken()
			if tc.badState {
				if err == nil || calls != 3 {
					t.Fatalf("mismatched state was accepted: %q, %v, %d calls", got, err, calls)
				}
				return
			}
			if err != nil || got != "access" || verifier == "" || challenge == "" {
				t.Fatalf("login failed: %q, %v", got, err)
			}
			wantCalls := 4
			if tc.refreshRejected {
				wantCalls++
			}
			if calls != wantCalls {
				t.Fatalf("got %d calls, want %d", calls, wantCalls)
			}
			if p.client.Jar != nil {
				t.Fatal("login cookies were not cleared")
			}
		})
	}
}

func TestCancelledAuthenticationDoesNotCallNetwork(t *testing.T) {
	p := NewTokenProvider("user", "password")
	p.client.Transport = tokenTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("network called after cancellation")
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetTokenContext(ctx); err == nil {
		t.Fatal("cancelled authentication succeeded")
	}
}
