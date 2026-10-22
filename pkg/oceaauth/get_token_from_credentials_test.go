package oceaauth

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type signInTransport struct{ body string }

func (s signInTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(s.body)), Header: make(http.Header)}, nil
}

func TestSubmitCredentialsChecksBodyStatus(t *testing.T) {
	for _, tc := range []struct {
		body      string
		wantError bool
	}{
		{`{"status":"200"}`, false},
		{`{"status":"400","message":"Invalid credentials"}`, true},
		{`{}`, true},
	} {
		provider := NewTokenProvider("test@example.com", "test-password")
		provider.client.Transport = signInTransport{tc.body}
		err := provider.submitCredentials(authorizeSettings{}, []*http.Cookie{{Name: "x-ms-cpim-csrf", Value: "test-csrf"}})
		if (err != nil) != tc.wantError {
			t.Errorf("response %s: unexpected error %v", tc.body, err)
		}
	}
}

func TestExtractAuthorizeSettingsMissingPageViewID(t *testing.T) {
	req, _ := http.NewRequest("GET", OCEALoginPage, nil)
	_, err := extractAuthorizeSettings([]byte(`{"transId":"StateProperties=abc","csrf":"abc"}`), req)
	if err == nil || !strings.Contains(err.Error(), "pageViewId") {
		t.Fatalf("expected missing pageViewId error, got %v", err)
	}
}

func TestExtractAuthorizeSettings(t *testing.T) {
	for _, page := range []string{
		`var SETTINGS = {"transId":"StateProperties=abc123","pageViewId":"1234-abcd","csrf":"abc_123=="};`,
		`var SETTINGS = {
  "transId": "StateProperties=abc123",
  "pageViewId": "1234-abcd",
  "csrf": "abc_123=="
};`,
	} {
		req, _ := http.NewRequest("GET", OCEALoginPage, nil)
		settings, err := extractAuthorizeSettings([]byte(page), req)
		if err != nil {
			t.Fatal(err)
		}
		if settings.transactionID != "StateProperties=abc123" || settings.pageViewID != "1234-abcd" || settings.csrf != "abc_123==" {
			t.Fatalf("unexpected settings: %+v", settings)
		}
	}
}
