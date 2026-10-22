package oceaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

const (
	OCEAAPIBaseURL = "https://espace-resident-api.ocea-sb.com/api/v1"
)

var (
	ErrMaintenance = errors.New("api is under maintenance")
)

type TokenProvider interface {
	GetToken() (string, error)
}

type APIClient struct {
	tokenProvider TokenProvider
	client        *http.Client
	ctx           context.Context
}

func NewClient(provider TokenProvider) APIClient {
	client := APIClient{
		tokenProvider: provider,
		client:        &http.Client{Timeout: 30 * time.Second},
		ctx:           context.Background(),
	}

	return client
}

func (o APIClient) GetResident() (Resident, error) {
	resident := Resident{}

	err := o.do("GET", OCEAAPIBaseURL+"/resident", nil, &resident)
	if err != nil {
		return resident, fmt.Errorf("failed to get resident: %w", err)
	}

	return resident, nil
}

func (o APIClient) GetLocal(localID string) (Local, error) {
	local := Local{}

	err := o.do("GET", OCEAAPIBaseURL+"/local/"+url.PathEscape(localID), nil, &local)
	if err != nil {
		return local, fmt.Errorf("failed to get local: %w", err)
	}

	return local, nil
}

type localIndexDemandRequest struct {
	LocalID string   `json:"localId"`
	Token   string   `json:"token"`
	PDSIDs  []string `json:"pdsIds"`
}

func (o APIClient) GetDevices(localID string, statementDate time.Time) ([]Device, error) {
	meters, err := o.GetMeters(localID)
	if err != nil {
		return nil, err
	}
	pdsIDs := make([]string, 0, len(meters))
	seen := make(map[string]bool)
	for _, meter := range meters {
		if meter.PDSID == "" {
			return nil, fmt.Errorf("meter has no pdsId")
		}
		if !seen[meter.PDSID] {
			pdsIDs = append(pdsIDs, meter.PDSID)
			seen[meter.PDSID] = true
		}
	}
	if len(pdsIDs) == 0 {
		return nil, fmt.Errorf("no meter found for local %s", localID)
	}

	token := ""

	t := statementDate
	if t.IsZero() {
		t = time.Now()
	}
	date := t.Format("2006-01-02")

	query := url.Values{
		"dateDemande":    {date + "T00:00:00.000Z"},
		"raisonConforme": {"RealisationEtatDesLieux"},
	}
	err = o.do("GET", OCEAAPIBaseURL+"/local/"+url.PathEscape(localID)+"/token?"+query.Encode(), nil, &token)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("failed to get token: (empty)")
	}

	// A pointer distinguishes a genuine zero from a missing/null reading.
	var readings []struct {
		Device
		Index *float64 `json:"valeurIndex"`
	}
	indexRequest := localIndexDemandRequest{
		LocalID: localID,
		Token:   token,
		PDSIDs:  pdsIDs,
	}

	err = o.do("POST", OCEAAPIBaseURL+"/local/demande", &indexRequest, &readings)
	if err != nil {
		return nil, fmt.Errorf("failed to get indexes: %w", err)
	}

	devices := make([]Device, 0, len(readings))
	for _, reading := range readings {
		if reading.Index == nil {
			continue
		}
		reading.Device.ValeurIndex = *reading.Index
		devices = append(devices, reading.Device)
	}
	return devices, nil
}

func (o APIClient) do(method, url string, request, response interface{}) error {
	body, err := o.doRequest(method, url, request, "application/json")
	if err != nil {
		return err
	}
	if response != nil {
		if err := json.Unmarshal(body, response); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}
	return nil
}

// WithContext binds portal calls, including authentication, to the worker lifetime.
func (o APIClient) WithContext(ctx context.Context) APIClient { o.ctx = ctx; return o }

// GetMeters is the current inventory; readings may contain only a subset of it.
func (o APIClient) GetMeters(localID string) ([]Meter, error) {
	var meters []Meter
	if err := o.do("GET", OCEAAPIBaseURL+"/local/"+url.PathEscape(localID)+"/appareils", nil, &meters); err != nil {
		return nil, fmt.Errorf("getting meters: %w", err)
	}
	if len(meters) == 0 {
		return nil, fmt.Errorf("no meter found for local %s", localID)
	}
	return meters, nil
}

const maxResponseBytes = 32 << 20

func (o APIClient) doRequest(method, endpoint string, request interface{}, accept string) ([]byte, error) {
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var payload io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	var token string
	if provider, ok := o.tokenProvider.(interface {
		GetTokenContext(context.Context) (string, error)
	}); ok {
		token, err = provider.GetTokenContext(ctx)
	} else {
		token, err = o.tokenProvider.GetToken()
	}
	if err != nil {
		return nil, fmt.Errorf("getting access token: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("empty access token")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	zap.L().Debug("HTTP request", zap.String("method", method), zap.String("path", req.URL.Path))
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing HTTP request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading HTTP response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("HTTP response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var maintenance struct {
			IsOnline *bool `json:"IsOnline"`
		}
		if json.Unmarshal(body, &maintenance) == nil && maintenance.IsOnline != nil && !*maintenance.IsOnline {
			return nil, ErrMaintenance
		}
		return nil, fmt.Errorf("HTTP request failed: status %d", resp.StatusCode)
	}
	return body, nil
}
