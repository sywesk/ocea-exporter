package oceaauth

import (
	"fmt"
	"net/http"
	"time"
)

const (
	OCEAPortalHome          = "https://espace-resident.ocea-sb.com"
	OCEALoginPage           = "https://osbespaceresident.b2clogin.com/osbespaceresident.onmicrosoft.com/b2c_1a_signup_signin/oauth2/v2.0/authorize"
	OCEATokenEndpoint       = "https://osbespaceresident.b2clogin.com/osbespaceresident.onmicrosoft.com/b2c_1a_signup_signin/oauth2/v2.0/token"
	OCEALoginSelfAssertPage = "https://osbespaceresident.b2clogin.com/osbespaceresident.onmicrosoft.com/B2C_1A_SIGNUP_SIGNIN/SelfAsserted"
	OCEALoginConfirmPage    = "https://osbespaceresident.b2clogin.com/osbespaceresident.onmicrosoft.com/B2C_1A_SIGNUP_SIGNIN/api/CombinedSigninAndSignup/confirmed"
	OCEALoginClientID       = "1cacfb15-0b3c-42cc-a662-736e4737e7d9"
	OCEALoginScope          = "https://osbespaceresident.onmicrosoft.com/app-imago-espace-resident-back-prod/user_impersonation openid profile offline_access"
	UserAgent               = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 Edg/107.0.1418.42"
)

type TokenProvider struct {
	client   *http.Client
	tokens   tokens
	username string
	password string
}

func NewTokenProvider(username, password string) *TokenProvider {
	return &TokenProvider{
		client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: 5 * time.Second,
		},
		username: username,
		password: password,
	}
}

func (o *TokenProvider) GetToken() (string, error) {
	now := time.Now().UTC().Unix()

	// If the access token is still valid, there's nothing to do, just return it.
	if o.tokens.AccessToken != "" && now < o.tokens.ExpiresOn-10 {
		return o.tokens.AccessToken, nil
	}

	// If we do not have a token, or the refresh token is too old, we need to do the whole flow again.
	// Otherwise, just refresh the token.
	if o.tokens.AccessToken == "" || now > o.tokens.RefreshTokenExpiresIn+o.tokens.NotBefore-10 {
		err := o.getTokenFromCredentials()
		if err != nil {
			return "", fmt.Errorf("failed to get token from credentials: %w", err)
		}
	} else {
		err := o.refreshToken()
		if err != nil {
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}
	}

	return o.tokens.AccessToken, nil
}
