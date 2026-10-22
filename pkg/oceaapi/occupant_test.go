package oceaapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

var exportDay = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
var exportMeters = []Meter{{ID: "hot", PDSID: "pds-hot", Serial: "hot-serial"}, {ID: "heat", PDSID: "pds-heat", Serial: "heat-serial"}}

func sampleExport(t *testing.T, missing bool) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, m := range exportMeters {
		name := m.Serial + " - readings"
		if _, err := f.NewSheet(name); err != nil {
			t.Fatal(err)
		}
		fluid, unit := "Eau Chaude", "m3"
		if i == 1 {
			fluid, unit = "Thermique Chaud", "KWh"
		}
		rows := [][]interface{}{
			{"Index de l'appareil " + m.Serial},
			{"Fluide", "Emplacement", "Date", "Index", "Unité"},
			{fluid, "", "22/10/2026", "", unit}, // Not reported yet.
			{fluid, "", "21/10/2026", 124.325, unit},
			{fluid, "", "20/10/2026", 120, unit}, // Unsorted history.
			{fluid, "", "23/10/2026", 999, unit}, // Future readings must be ignored.
		}
		if i == 1 {
			rows[2][3] = 0 // A real zero must be kept.
			if missing {
				rows[2][3], rows[3][3] = "", ""
			}
		}
		for j, row := range rows {
			if err := f.SetSheetRow(name, fmt.Sprintf("A%d", j+1), &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.DeleteSheet("Sheet1"); err != nil {
		t.Fatal(err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOccupantExport(t *testing.T) {
	devices, err := parseOccupantExport(sampleExport(t, false), exportMeters, exportDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].AppareilID != "hot" || devices[0].ValeurIndex != 124.325 || devices[0].Date != "2026-10-21T00:00:00" || devices[0].Fluide != "EauChaude" || devices[1].ValeurIndex != 0 || devices[1].Fluide != "Cetc" || devices[1].Date != "2026-10-22T00:00:00" {
		t.Fatalf("unexpected readings: %+v", devices)
	}
	if _, err := parseOccupantExport(sampleExport(t, true), exportMeters, exportDay); err == nil {
		t.Fatal("expected missing meter error")
	}
	if _, err := parseOccupantExport([]byte(`{"error":"no export"}`), exportMeters, exportDay); err == nil {
		t.Fatal("expected invalid workbook error")
	}
}

func TestOccupantExportInvalidCells(t *testing.T) {
	for _, tc := range []struct {
		name, cell string
		value      interface{}
	}{
		{"bad header", "A2", "Not Fluide"},
		{"bad date", "C4", "not-a-date"},
		{"bad index", "D4", "not-an-index"},
		{"nan", "D4", "NaN"},
		{"infinity", "D4", "+Inf"},
		{"negative index", "D4", -1},
		{"unknown fluid", "A4", "Unknown"},
		{"wrong unit", "E4", "litres"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			book, err := excelize.OpenReader(bytes.NewReader(sampleExport(t, false)))
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			if err := book.SetCellValue("hot-serial - readings", tc.cell, tc.value); err != nil {
				t.Fatal(err)
			}
			data, err := book.WriteToBuffer()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseOccupantExport(data.Bytes(), exportMeters, exportDay); err == nil {
				t.Fatal("accepted malformed export")
			}
		})
	}
}

func TestOccupantExportDecimalCommaAndWhitespace(t *testing.T) {
	book, err := excelize.OpenReader(bytes.NewReader(sampleExport(t, false)))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for cell, value := range map[string]string{"A4": " Eau Chaude ", "C4": " 21/10/2026 ", "D4": " 124,325 ", "E4": " m3 "} {
		if err := book.SetCellValue("hot-serial - readings", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	devices, err := parseOccupantExport(data.Bytes(), exportMeters, exportDay)
	if err != nil || devices[0].ValeurIndex != 124.325 {
		t.Fatalf("valid export rejected: %+v, %v", devices, err)
	}
}

func TestGetOccupantDevices(t *testing.T) {
	data := sampleExport(t, false)
	for _, noContent := range []bool{false, true} {
		t.Run(fmt.Sprintf("noContent=%t", noContent), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-access-token" {
					t.Error("missing authorization")
				}
				switch r.URL.Path {
				case "/api/v1/local/home/appareils":
					json.NewEncoder(w).Encode(exportMeters)
				case "/api/v1/local/demande/export":
					if r.Method != "POST" {
						t.Error("expected POST")
					}
					var request struct {
						LocalID string   `json:"localId"`
						Period  period   `json:"periode"`
						PDSIDs  []string `json:"pdsIds"`
						Token   string   `json:"token"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if request.LocalID != "home" || request.Period.Start != "2026-10-21T00:00:00.000Z" || request.Period.End != "2026-10-22T00:00:00.000Z" || request.Token != "" || !reflect.DeepEqual(request.PDSIDs, []string{"pds-hot", "pds-heat"}) {
						t.Errorf("unexpected export request: %+v", request)
					}
					if noContent {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
					w.Write(data)
				default:
					t.Errorf("unexpected owner endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(testTokenProvider{})
			client.client = &http.Client{Transport: redirectTransport{server.Listener.Addr().String()}}
			devices, err := client.GetOccupantDevices("home", exportDay)
			if noContent && err == nil || !noContent && (err != nil || len(devices) != 2) {
				t.Fatalf("unexpected result: %+v, %v", devices, err)
			}
		})
	}
}

// Optional local regression against a private HAR; no capture data is committed.
func TestOccupantHAR(t *testing.T) {
	path := os.Getenv("OCEA_TEST_HAR")
	if path == "" {
		t.Skip("set OCEA_TEST_HAR to check a local capture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL    string `json:"url"`
					Method string `json:"method"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Text     string `json:"text"`
						Encoding string `json:"encoding"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &har); err != nil {
		t.Fatal(err)
	}
	var meters []Meter
	var workbook []byte
	var owner []Device
	for _, entry := range har.Log.Entries {
		if entry.Response.Content.Text == "" {
			continue
		}
		if entry.Request.Method == "GET" && strings.HasSuffix(entry.Request.URL, "/appareils") {
			if err := json.Unmarshal([]byte(entry.Response.Content.Text), &meters); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasSuffix(entry.Request.URL, "/demande/export") && entry.Response.Status == 200 {
			workbook, err = base64.StdEncoding.DecodeString(entry.Response.Content.Text)
			if err != nil {
				t.Fatal(err)
			}
		}
		if entry.Request.Method == "POST" && strings.HasSuffix(entry.Request.URL, "/demande") && entry.Response.Status == 200 {
			var readings []Device
			if err := json.Unmarshal([]byte(entry.Response.Content.Text), &readings); err != nil {
				t.Fatal(err)
			}
			if len(readings) > 0 {
				owner = readings
			}
		}
	}
	if len(workbook) == 0 || len(meters) == 0 || len(owner) == 0 {
		t.Fatal("capture is missing export, meters or owner readings")
	}
	devices, err := parseOccupantExport(workbook, meters, exportDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != len(owner) {
		t.Fatal("owner and occupant meter counts differ")
	}
	for _, reading := range devices {
		found := false
		for _, expected := range owner {
			if reading.AppareilID == expected.AppareilID {
				found = true
				if reading.ValeurIndex != expected.ValeurIndex || reading.NumeroCompteurAppareil != expected.NumeroCompteurAppareil || reading.Fluide != expected.Fluide || reading.Unite != expected.Unite || reading.Date[:10] != expected.Date[:10] {
					t.Fatal("owner and occupant readings differ")
				}
			}
		}
		if !found {
			t.Fatal("unmatched meter")
		}
	}
}
