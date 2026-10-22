package oceaapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testTokenProvider struct{}

func (testTokenProvider) GetToken() (string, error) { return "test-access-token", nil }

type redirectTransport struct {
	server string
}

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = r.server
	return http.DefaultTransport.RoundTrip(proxyReq)
}

func TestGetDevicesOwnerFlow(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%t", empty), func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Header.Get("Authorization") != "Bearer test-access-token" {
					t.Error("missing access token")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/local/home/appareils":
					fmt.Fprint(w, `[{"id":"hot","pdsId":"pds-hot"},{"id":"cold","pdsId":"pds-cold"},{"id":"heat","pdsId":"pds-heat"}]`)
				case "/api/v1/local/home/token":
					if r.URL.Query().Get("dateDemande") != "2026-10-21T00:00:00.000Z" || r.URL.Query().Get("raisonConforme") != "RealisationEtatDesLieux" {
						t.Errorf("unexpected query: %s", r.URL.RawQuery)
					}
					fmt.Fprint(w, `"test-demand-token"`)
				case "/api/v1/local/demande":
					var body localIndexDemandRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.LocalID != "home" || body.Token != "test-demand-token" || !reflect.DeepEqual(body.PDSIDs, []string{"pds-hot", "pds-cold", "pds-heat"}) {
						t.Errorf("unexpected demand: %+v", body)
					}
					if empty {
						fmt.Fprint(w, `[]`)
					} else {
						fmt.Fprint(w, `[{"appareilId":"cold","numeroCompteurAppareil":"cold-serial","valeurIndex":248.642,"unite":"m3","fluide":"EauFroide","date":"2026-10-21T08:00:15"}]`)
					}
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(testTokenProvider{})
			client.client = &http.Client{Transport: redirectTransport{server.Listener.Addr().String()}}
			devices, err := client.GetDevices("home", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if empty && len(devices) != 0 || !empty && (len(devices) != 1 || devices[0].ValeurIndex != 248.642 || devices[0].NumeroCompteurAppareil != "cold-serial") {
				t.Fatalf("unexpected readings: %+v", devices)
			}
			if len(calls) != 3 {
				t.Fatalf("unexpected requests: %v", calls)
			}
		})
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		maintenance bool
	}{
		{"offline", `{"IsOnline":false}`, true},
		{"ordinary JSON error", `{"message":"forbidden"}`, false},
		{"online", `{"IsOnline":true}`, false},
		{"HTML error", `<html>unavailable</html>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := NewClient(testTokenProvider{})
			err := client.do("GET", server.URL, nil, nil)
			if err == nil || errors.Is(err, ErrMaintenance) != tc.maintenance {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRequestCancellationReachesHTTPTransport(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	defer server.CloseClientConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient(testTokenProvider{}).WithContext(ctx)
	done := make(chan error, 1)
	go func() { done <- client.do("GET", server.URL, nil, nil) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request was not sent")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP request ignored cancellation")
	}
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOversizedHTTPResponseIsRejected(t *testing.T) {
	client := NewClient(testTokenProvider{})
	client.client.Transport = responseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponseBytes+1))), Header: make(http.Header)}, nil
	})
	if err := client.do("GET", "https://example.invalid", nil, nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("response limit not enforced: %v", err)
	}
}

func TestOwnerMissingIndexesAreNotZeroReadings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/appareils"):
			fmt.Fprint(w, `[{"id":"zero","pdsId":"pds"}]`)
		case strings.HasSuffix(r.URL.Path, "/token"):
			fmt.Fprint(w, `"demand-token"`)
		case strings.HasSuffix(r.URL.Path, "/demande"):
			fmt.Fprint(w, `[{"appareilId":"zero","numeroCompteurAppareil":"serial","fluide":"EauFroide","valeurIndex":0},{"appareilId":"null","valeurIndex":null},{"appareilId":"missing"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(testTokenProvider{})
	client.client.Transport = redirectTransport{server.Listener.Addr().String()}
	devices, err := client.GetDevices("home", time.Now())
	if err != nil || len(devices) != 1 || devices[0].AppareilID != "zero" || devices[0].NumeroCompteurAppareil != "serial" || devices[0].Fluide != FluidColdWater || devices[0].ValeurIndex != 0 {
		t.Fatalf("missing index confused with zero: %+v, %v", devices, err)
	}
}
