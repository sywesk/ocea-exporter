package oceaauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
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
	mu       sync.Mutex
	ctx      context.Context
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

func (o *TokenProvider) GetToken() (string, error) { return o.GetTokenContext(context.Background()) }

func (o *TokenProvider) GetTokenContext(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	o.ctx = ctx
	defer func() { o.ctx = nil }()
	now := time.Now().Unix()
	if o.tokens.AccessToken != "" && now < o.tokens.ExpiresOn-10 {
		return o.tokens.AccessToken, nil
	}
	refreshValid := o.tokens.RefreshToken != "" && (o.tokens.RefreshTokenExpiresIn == 0 || now < o.tokens.NotBefore+o.tokens.RefreshTokenExpiresIn-10)
	if refreshValid {
		err := o.refreshToken()
		if err == nil {
			return o.tokens.AccessToken, nil
		}
		if !errors.Is(err, errRefreshRejected) {
			return "", err
		}
	}
	if err := o.getTokenFromCredentials(); err != nil {
		return "", fmt.Errorf("getting token from credentials: %w", err)
	}
	return o.tokens.AccessToken, nil
}

func (o *TokenProvider) newRequest(method, endpoint string, body io.Reader) (*http.Request, error) {
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return http.NewRequestWithContext(ctx, method, endpoint, body)
}
