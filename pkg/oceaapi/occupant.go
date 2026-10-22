package oceaapi

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type meter struct {
	ID     string `json:"id"`
	PDSID  string `json:"pdsId"`
	Serial string `json:"numeroSerie"`
}

type period struct {
	Start string `json:"dateDebut"`
	End   string `json:"dateFin"`
}

// GetOccupantDevices reads the latest index per meter from the period export.
// Include yesterday because today's readings may not have reached the portal yet.
func (o APIClient) GetOccupantDevices(localID string, date time.Time) ([]Device, error) {
	if date.IsZero() {
		date = time.Now()
	}
	var meters []meter
	if err := o.do("GET", OCEAAPIBaseURL+"/local/"+localID+"/appareils", nil, &meters); err != nil {
		return nil, fmt.Errorf("failed to get meters: %w", err)
	}
	pdsIDs := make([]string, 0, len(meters))
	seen := make(map[string]bool)
	for _, m := range meters {
		if m.ID == "" || m.PDSID == "" || m.Serial == "" {
			return nil, fmt.Errorf("incomplete meter metadata")
		}
		if !seen[m.PDSID] {
			pdsIDs = append(pdsIDs, m.PDSID)
			seen[m.PDSID] = true
		}
	}
	if len(meters) == 0 {
		return nil, fmt.Errorf("no meter found for local %s", localID)
	}
	request := struct {
		LocalID string   `json:"localId"`
		Period  period   `json:"periode"`
		PDSIDs  []string `json:"pdsIds"`
	}{localID, period{date.AddDate(0, 0, -1).Format("2006-01-02") + "T00:00:00.000Z", date.Format("2006-01-02") + "T00:00:00.000Z"}, pdsIDs}
	body, err := o.doRequest("POST", OCEAAPIBaseURL+"/local/demande/export", request, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err != nil {
		return nil, fmt.Errorf("failed to export occupant indexes: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("occupant export contains no readings")
	}
	return parseOccupantExport(body, meters, date)
}

func parseOccupantExport(data []byte, meters []meter, date time.Time) ([]Device, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to open occupant Excel export: %w", err)
	}
	defer workbook.Close()
	bySerial := make(map[string]meter, len(meters))
	for _, m := range meters {
		bySerial[m.Serial] = m
	}
	latest := make(map[string]Device)
	cutoff, _ := time.Parse("2006-01-02", date.Format("2006-01-02"))
	start := cutoff.AddDate(0, 0, -1)
	for _, sheet := range workbook.GetSheetList() {
		serial, _, ok := strings.Cut(sheet, " - ")
		m, known := bySerial[serial]
		if !ok || !known {
			return nil, fmt.Errorf("unrecognized meter sheet %q", sheet)
		}
		rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, fmt.Errorf("failed to read sheet %q: %w", sheet, err)
		}
		if len(rows) < 2 || len(rows[1]) < 5 || rows[1][0] != "Fluide" || rows[1][2] != "Date" || rows[1][3] != "Index" || rows[1][4] != "Unité" {
			return nil, fmt.Errorf("unexpected export columns in sheet %q", sheet)
		}
		for i, row := range rows[2:] {
			if len(row) == 0 {
				continue
			}
			if len(row) < 4 || strings.TrimSpace(row[3]) == "" {
				continue // A missing index is not a zero reading.
			}
			if len(row) < 5 {
				return nil, fmt.Errorf("missing unit in sheet %q row %d", sheet, i+3)
			}
			day, err := time.Parse("02/01/2006", row[2])
			if err != nil {
				return nil, fmt.Errorf("invalid date in sheet %q row %d: %w", sheet, i+3, err)
			}
			if day.Before(start) || day.After(cutoff) {
				continue
			}
			value, err := strconv.ParseFloat(strings.ReplaceAll(row[3], ",", "."), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("invalid index in sheet %q row %d", sheet, i+3)
			}
			fluid, ok := map[string]string{"Eau Chaude": "EauChaude", "Eau Froide": "EauFroide", "Thermique Chaud": "Cetc"}[row[0]]
			if !ok {
				return nil, fmt.Errorf("unknown fluid %q in sheet %q", row[0], sheet)
			}
			stamp := day.Format("2006-01-02T15:04:05")
			if previous, ok := latest[serial]; !ok || stamp > previous.Date {
				latest[serial] = Device{AppareilID: m.ID, NumeroCompteurAppareil: serial, Date: stamp, Fluide: fluid, ValeurIndex: value, Emplacement: row[1], Unite: row[4]}
			}
		}
	}
	devices := make([]Device, 0, len(meters))
	for _, m := range meters {
		reading, ok := latest[m.Serial]
		if !ok {
			return nil, fmt.Errorf("no reading for meter %s in occupant export for today or yesterday", m.Serial)
		}
		devices = append(devices, reading)
	}
	return devices, nil
}
