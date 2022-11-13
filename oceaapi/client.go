package oceaapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	OCEAAPIBaseURL = "https://espace-resident-api.ocea-sb.com/api/v1"
)

type TokenProvider interface {
	GetToken() (string, error)
}

type APIClient struct {
	tokenProvider TokenProvider
	client        *http.Client
}

func NewClient(provider TokenProvider) APIClient {
	client := APIClient{
		tokenProvider: provider,
		client:        http.DefaultClient,
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

func (o APIClient) GetFluidDashboard(localID, fluid string) (Dashboard, error) {
	dashboard := Dashboard{}

	err := o.do("GET", OCEAAPIBaseURL+"/local/"+localID+"/conso/dashboard/"+fluid, nil, &dashboard)
	if err != nil {
		return dashboard, fmt.Errorf("failed to get dashboard: %w", err)
	}

	return dashboard, nil
}

func (o APIClient) do(method, url string, request, response interface{}) error {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}

	if request != nil {
		reqBytes, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req.Body = io.NopCloser(bytes.NewReader(reqBytes))
	}

	token, err := o.tokenProvider.GetToken()
	if err != nil {
		return fmt.Errorf("failed to get token from provider: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to do HTTP request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
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
