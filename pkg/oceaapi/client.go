package oceaapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	OCEAAPIBaseURL = "https://espace-resident-api.ocea-sb.com/api/v1"
)

var (
	ErrMaintenance = fmt.Errorf("api is under maintenance")
)

type TokenProvider interface {
	GetToken() (string, error)
}

type APIClient struct {
	tokenProvider TokenProvider
	client        *http.Client
}

type MaintenanceResponse struct {
	IsOnline           bool   `json:"IsOnline"`
	MaintenancePageUrl string `json:"MaintenancePageUrl"`
	ErrorMessage       string `json:"ErrorMessage"`
}

func NewClient(provider TokenProvider) APIClient {
	client := APIClient{
		tokenProvider: provider,
		client:        &http.Client{Timeout: 30 * time.Second},
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

	err := o.do("GET", OCEAAPIBaseURL+"/local/"+localID, nil, &local)
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
	var meters []struct {
		PDSID string `json:"pdsId"`
	}
	if err := o.do("GET", OCEAAPIBaseURL+"/local/"+localID+"/appareils", nil, &meters); err != nil {
		return nil, fmt.Errorf("failed to get meters: %w", err)
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
	err := o.do("GET", OCEAAPIBaseURL+"/local/"+localID+"/token?"+query.Encode(), nil, &token)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	} else if token == "" {
		return nil, fmt.Errorf("failed to get token: (empty)")
	}

	var deviceList []Device
	indexRequest := localIndexDemandRequest{
		LocalID: localID,
		Token:   token,
		PDSIDs:  pdsIDs,
	}

	err = o.do("POST", OCEAAPIBaseURL+"/local/demande", &indexRequest, &deviceList)
	if err != nil {
		return nil, fmt.Errorf("failed to get indexes: %w", err)
	}

	return deviceList, nil
}

func (o APIClient) do(method, url string, request, response interface{}) error {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}
	zap.L().Debug("HTTP request", zap.String("url", url), zap.String("method", method))

	if request != nil {
		reqBytes, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req.Body = io.NopCloser(bytes.NewReader(reqBytes))
		req.Header.Set("Content-Type", "application/json")
	}

	token, err := o.tokenProvider.GetToken()
	if err != nil {
		return fmt.Errorf("failed to get token from provider: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	if response != nil {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to do HTTP request: %w", err)
	}
	defer resp.Body.Close()

	zap.L().Debug("HTTP response status", zap.String("status", resp.Status))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isMaintenanceError(resp) {
			return ErrMaintenance
		}
		return fmt.Errorf("HTTP request failed: invalid status code %d (%s)", resp.StatusCode, resp.Status)
	}

	if response != nil {
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read all response bytes: %w", err)
		}

		err = json.Unmarshal(respBytes, response)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func isMaintenanceError(resp *http.Response) bool {
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		zap.L().Error("failed to read api error response", zap.Error(err))
		return false
	}
	maintenanceResponse := &struct {
		IsOnline *bool `json:"IsOnline"`
	}{}
	err = json.Unmarshal(respBytes, maintenanceResponse)
	if err != nil {
		return false
	}

	return maintenanceResponse.IsOnline != nil && !*maintenanceResponse.IsOnline
}
