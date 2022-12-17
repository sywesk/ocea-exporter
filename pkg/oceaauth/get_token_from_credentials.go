package oceaauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

/*

	getTokenFromCredentials will simulate a user's action on the oauth2 portal, to exchange credentials (username,
	password) for oauth2 tokens.

	Here's the overall flow:

		Warning: We need a cookie jar to save & inject cookies automatically. If not done, the flow will just fail.

		1. We get the 'authorize' page.

			See TokenProvider.getAuthorizePage(). To get to this page, we need to craft some calling arguments like the
			PKCE challenge (code_challenge, code_verifier, code_challenge_method) and request id. For the rest we just
			reuse the values found in network dumps.

			Note that we need to extract some values from the embedded JSON object `SETTINGS`:
				- csrf token
				- transaction id
				- page view id (I believe this one is optional though)
			See extractAuthorizeSettings().

		2. We then simulate the form submit.

			See TokenProvider.submitCredentials(). We send a simple form to a given endpoint, and get a 200. nothing
			much happens here.

		3. After that, we "confirm" the login

			See TokenProvider.confirmLogin(). This call needs the csrf token and the transaction id. If everything goes
			well, we receive a redirect request containing the code in its fragment. This is parsed by extractAuthCode()

		4. Finally, we exchange the code for the access/refresh tokens pair

			See TokenProvider.exchangeCode(). This call will need the code_verifier generated earlier and the code we
			got from the previous call.


	Appendix:

		PKCE challenge

			1. Generate a random string.
			2. Hash it with the base64 url encoding (RawUrlEncoding in go), if using the S256 challenge method.
			3. Send the hash to the authorize page, and keep the original string.
			4. When comes the moment to exchange the code for a real access_token, also give the original string.

*/

func (o *TokenProvider) getTokenFromCredentials() error {
	o.client.Jar, _ = cookiejar.New(nil)

	requestId := uuid.NewString()
	challengeCleartext, challengeHash, err := genChallenge()
	if err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	authSettings, cookies, err := o.getAuthorizePage(requestId, challengeHash)
	if err != nil {
		return fmt.Errorf("failed to get portal page: %w", err)
	}

	err = o.submitCredentials(authSettings, cookies)
	if err != nil {
		return fmt.Errorf("failed to submit credentials: %w", err)
	}

	authCode, err := o.confirmLogin(authSettings)
	if err != nil {
		return fmt.Errorf("failed to confirm login: %w", err)
	}

	o.client.Jar = nil

	o.tokens, err = o.exchangeCode(authCode, requestId, challengeCleartext)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}

	zap.L().Info("auth: got token from credentials")
	return nil
}

func (o *TokenProvider) exchangeCode(authCode authCodeResponse, requestId string, challengeCleartext string) (tokens, error) {
	form := url.Values{}

	form.Add("client_id", OCEALoginClientID)
	form.Add("redirect_uri", OCEAPortalHome)
	form.Add("scope", OCEALoginScope)
	form.Add("code", authCode.code)
	form.Add("code_verifier", challengeCleartext)
	form.Add("grant_type", "authorization_code")
	form.Add("client_info", "1")
	form.Add("client-request-id", requestId)

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
		return tokens{}, fmt.Errorf("failed exchange code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return tokens{}, fmt.Errorf("failed exchange code: bad status code (expected 200): %d", resp.StatusCode)
	}

	return extractTokens(resp)
}

func (o *TokenProvider) confirmLogin(authSettings authorizeSettings) (authCodeResponse, error) {
	req, err := http.NewRequest("GET", OCEALoginConfirmPage, nil)
	if err != nil {
		return authCodeResponse{}, fmt.Errorf("failed to create http request: %w", err)
	}

	q := req.URL.Query()

	q.Set("p", "B2C_1A_SIGNUP_SIGNIN")
	q.Set("rememberMe", "false")
	q.Set("csrf_token", authSettings.crsf)
	q.Set("diags", "{\"pageViewId\":\""+authSettings.pageViewId+"\",\"pageId\":\"CombinedSigninAndSignup\",\"trace\":[]}")

	req.URL.RawQuery = q.Encode() + "&tx=" + authSettings.transId

	req.Header.Set("Origin", "https://osbespaceresident.b2clogin.com")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Referer", authSettings.pageUrl)

	resp, err := o.client.Do(req)
	if err != nil {
		return authCodeResponse{}, fmt.Errorf("failed confirm login: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 302 {
		return authCodeResponse{}, fmt.Errorf("failed confirm login: bad status code (expected 302): %d", resp.StatusCode)
	}

	return extractAuthCode(resp)
}

func (o *TokenProvider) submitCredentials(authSettings authorizeSettings, cookies []*http.Cookie) error {
	form := url.Values{}

	form.Add("request_type", "RESPONSE")
	form.Add("email", o.username)
	form.Add("password", o.password)

	req, err := http.NewRequest("POST", OCEALoginSelfAssertPage, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	q := req.URL.Query()

	q.Set("tx", authSettings.transId)
	q.Set("p", "B2C_1A_SIGNUP_SIGNIN")

	req.URL.RawQuery = q.Encode()

	csrfToken := ""
	for _, cookie := range cookies {
		if strings.ToLower(cookie.Name) == "x-ms-cpim-csrf" {
			csrfToken = cookie.Value
		}
	}

	if csrfToken == "" {
		return fmt.Errorf("csrf token not found within cookies: %w", err)
	}

	req.Header.Set("Origin", "https://osbespaceresident.b2clogin.com")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-CSRF-TOKEN", csrfToken)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", authSettings.pageUrl)

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get authorize page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to authorize: bad status code: %d", resp.StatusCode)
	}

	return nil
}

func (o *TokenProvider) getAuthorizePage(requestId string, challengeHash string) (authorizeSettings, []*http.Cookie, error) {
	req, err := http.NewRequest("GET", OCEALoginPage, nil)
	if err != nil {
		return authorizeSettings{}, nil, fmt.Errorf("failed to create http request: %w", err)
	}

	loginState, err := genLoginState()
	if err != nil {
		return authorizeSettings{}, nil, fmt.Errorf("failed to generate login state: %w", err)
	}

	req.Header.Set("User-Agent", UserAgent)

	q := req.URL.Query()

	q.Set("client_id", OCEALoginClientID)
	q.Set("scope", OCEALoginScope)
	q.Set("redirect_uri", OCEAPortalHome)
	q.Set("client-request-id", requestId)
	q.Set("response_mode", "fragment")
	q.Set("response_type", "code")
	q.Set("x-client-SKU", "msal.js.browser")
	q.Set("x-client-VER", "2.28.1")
	q.Set("client_info", "1")
	q.Set("code_challenge", challengeHash)
	q.Set("code_challenge_method", "S256")
	q.Set("nonce", uuid.NewString()) // should be random
	q.Set("state", loginState)

	req.URL.RawQuery = q.Encode()

	resp, err := o.client.Do(req)
	if err != nil {
		return authorizeSettings{}, nil, fmt.Errorf("failed to get authorize page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return authorizeSettings{}, nil, fmt.Errorf("failed to get ocea portal: bad status code: %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return authorizeSettings{}, nil, fmt.Errorf("failed to read portal response: %w", err)
	}

	settings, err := extractAuthorizeSettings(respBytes, req)
	if err != nil {
		return authorizeSettings{}, nil, fmt.Errorf("failed to extract authorize settings: %w", err)
	}

	return settings, resp.Cookies(), nil
}

type loginState struct {
	ID   string `json:"id"`
	Meta struct {
		InteractionType string `json:"interactionType"`
	} `json:"meta"`
}

func genLoginState() (string, error) {
	state := loginState{
		ID: uuid.NewString(),
	}
	state.Meta.InteractionType = "redirect"

	payload, err := json.Marshal(&state)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login state: %w", err)
	}

	return base64.StdEncoding.EncodeToString(payload), nil
}

type authorizeSettings struct {
	transId    string
	pageViewId string
	crsf       string
	pageUrl    string
}

var (
	transIdRegex    = regexp.MustCompile(".*\"transId\":\"(StateProperties=[a-zA-Z0-9]+)\".*")
	pageViewIdRegex = regexp.MustCompile(".*\"pageViewId\":\"([a-f0-9-]+)\".*")
	csrfRegex       = regexp.MustCompile(".*\"csrf\":\"([a-zA-Z0-9=_-]+)\".*")
)

func extractAuthorizeSettings(page []byte, req *http.Request) (authorizeSettings, error) {
	transId := transIdRegex.FindSubmatch(page)
	if transId == nil {
		return authorizeSettings{}, fmt.Errorf("transId not found")
	}

	pageViewId := pageViewIdRegex.FindSubmatch(page)
	if transId == nil {
		return authorizeSettings{}, fmt.Errorf("pageViewId not found")
	}

	csrf := csrfRegex.FindSubmatch(page)
	if csrf == nil {
		return authorizeSettings{}, fmt.Errorf("csrf not found")
	}

	settings := authorizeSettings{
		transId:    string(transId[1]),
		pageViewId: string(pageViewId[1]),
		crsf:       string(csrf[1]),
		pageUrl:    req.URL.String(),
	}

	return settings, nil
}

type authCodeResponse struct {
	state      string
	clientInfo string
	code       string
}

func extractAuthCode(resp *http.Response) (authCodeResponse, error) {
	rawRedirectURL := resp.Header.Get("Location")
	if rawRedirectURL == "" {
		return authCodeResponse{}, fmt.Errorf("no location header in response")
	}

	redirectURL, err := url.Parse(rawRedirectURL)
	if err != nil {
		return authCodeResponse{}, fmt.Errorf("failed to parse location header: %w", err)
	}

	values, err := url.ParseQuery(redirectURL.Fragment)
	if err != nil {
		return authCodeResponse{}, fmt.Errorf("failed to parse location header fragment: %w", err)
	}

	return authCodeResponse{
		state:      values.Get("state"),
		clientInfo: values.Get("client_info"),
		code:       values.Get("code"),
	}, nil
}

func genChallenge() (string, string, error) {
	cleartext, err := genRandomString(43)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate challenge cleartext: %w", err)
	}

	// Prefix for easier debugging
	cleartext = "clear_" + cleartext

	sum := sha256.Sum256([]byte(cleartext))
	b64sum := base64.RawURLEncoding.EncodeToString(sum[:])

	return cleartext, b64sum, nil
}

type tokens struct {
	AccessToken           string `json:"access_token"`
	IdToken               string `json:"id_token"`
	TokenType             string `json:"token_type"`
	NotBefore             int64  `json:"not_before"`
	ExpiresIn             int64  `json:"expires_in"`
	ExpiresOn             int64  `json:"expires_on"`
	Resource              string `json:"resource"`
	ClientInfo            string `json:"client_info"`
	Scope                 string `json:"scope"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

func extractTokens(resp *http.Response) (tokens, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokens{}, fmt.Errorf("failed to extract tokens: %w", err)
	}

	result := tokens{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		return tokens{}, fmt.Errorf("failed to unmarshal tokens: %w", err)
	}

	return result, nil
}
