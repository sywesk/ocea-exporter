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

type Meter struct {
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
	meters, err := o.GetMeters(localID)
	if err != nil {
		return nil, err
	}
	pdsIDs := make([]string, 0, len(meters))
	seen := make(map[string]bool)
	serials := make(map[string]bool)
	ids := make(map[string]bool)
	for _, m := range meters {
		if m.ID == "" || m.PDSID == "" || m.Serial == "" {
			return nil, fmt.Errorf("incomplete meter metadata")
		}
		if serials[m.Serial] || ids[m.ID] {
			return nil, fmt.Errorf("duplicate meter metadata")
		}
		serials[m.Serial] = true
		ids[m.ID] = true
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

const maxExportRows = 10000

func parseOccupantExport(data []byte, meters []Meter, date time.Time) ([]Device, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 8 << 20})
	if err != nil {
		return nil, fmt.Errorf("opening occupant Excel export: %w", err)
	}
	defer workbook.Close()
	bySerial := make(map[string]Meter, len(meters))
	for _, m := range meters {
		if m.Serial == "" || m.ID == "" {
			return nil, fmt.Errorf("incomplete meter metadata")
		}
		if _, exists := bySerial[m.Serial]; exists {
			return nil, fmt.Errorf("duplicate serial %s", m.Serial)
		}
		bySerial[m.Serial] = m
	}
	latest := make(map[string]Device)
	cutoff, err := time.Parse("2006-01-02", date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("invalid export day: %w", err)
	}
	for _, sheet := range workbook.GetSheetList() {
		serial, _, ok := strings.Cut(sheet, " - ")
		m, known := bySerial[serial]
		if !ok || !known {
			return nil, fmt.Errorf("unrecognized meter sheet %q", sheet)
		}
		reading, err := readOccupantSheet(workbook, sheet, m, cutoff)
		if err != nil {
			return nil, err
		}
		if previous, ok := latest[serial]; !ok || reading.Date > previous.Date {
			latest[serial] = reading
		}
	}
	devices := make([]Device, 0, len(meters))
	for _, m := range meters {
		reading, ok := latest[m.Serial]
		if !ok {
			return nil, fmt.Errorf("no reading for meter %s in occupant export", m.Serial)
		}
		devices = append(devices, reading)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("empty occupant export")
	}
	return devices, nil
}

func readOccupantSheet(workbook *excelize.File, sheet string, m Meter, cutoff time.Time) (Device, error) {
	rows, err := workbook.Rows(sheet)
	if err != nil {
		return Device{}, fmt.Errorf("opening sheet %q: %w", sheet, err)
	}
	defer rows.Close()
	var latest Device
	rowNumber := 0
	for rows.Next() {
		rowNumber++
		if rowNumber > maxExportRows {
			return Device{}, fmt.Errorf("too many rows in sheet %q", sheet)
		}
		columns, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return Device{}, fmt.Errorf("reading sheet %q row %d: %w", sheet, rowNumber, err)
		}
		if rowNumber == 1 {
			continue
		}
		if rowNumber == 2 {
			if err := validateExportHeader(columns); err != nil {
				return Device{}, fmt.Errorf("sheet %q: %w", sheet, err)
			}
			continue
		}
		reading, present, err := parseOccupantRow(columns, m, cutoff)
		if err != nil {
			return Device{}, fmt.Errorf("sheet %q row %d: %w", sheet, rowNumber, err)
		}
		if present && (latest.Date == "" || reading.Date > latest.Date) {
			latest = reading
		}
	}
	if err := rows.Error(); err != nil {
		return Device{}, fmt.Errorf("reading sheet %q: %w", sheet, err)
	}
	if latest.Date == "" {
		return Device{}, fmt.Errorf("no reading for meter %s for today or yesterday", m.Serial)
	}
	return latest, nil
}

func validateExportHeader(row []string) error {
	for index, want := range map[int]string{0: "Fluide", 2: "Date", 3: "Index", 4: "Unité"} {
		if len(row) <= index || strings.TrimSpace(row[index]) != want {
			return fmt.Errorf("unexpected export columns")
		}
	}
	return nil
}

func parseOccupantRow(row []string, m Meter, cutoff time.Time) (Device, bool, error) {
	if len(row) < 4 || strings.TrimSpace(row[3]) == "" {
		return Device{}, false, nil
	}
	if len(row) < 5 {
		return Device{}, false, fmt.Errorf("missing unit")
	}
	day, err := time.Parse("02/01/2006", strings.TrimSpace(row[2]))
	if err != nil {
		return Device{}, false, fmt.Errorf("invalid date: %w", err)
	}
	if day.Before(cutoff.AddDate(0, 0, -1)) || day.After(cutoff) {
		return Device{}, false, nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(row[3]), ",", "."), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return Device{}, false, fmt.Errorf("invalid index")
	}
	fluid, ok := normalizeFluid(strings.TrimSpace(row[0]))
	if !ok {
		return Device{}, false, fmt.Errorf("unknown fluid %q", row[0])
	}
	unit := strings.TrimSpace(row[4])
	if fluid == FluidHeating {
		if !strings.EqualFold(unit, "kWh") {
			return Device{}, false, fmt.Errorf("unexpected heating unit %q", unit)
		}
	} else if unit != "m3" && unit != "m³" {
		return Device{}, false, fmt.Errorf("unexpected water unit %q", unit)
	}
	return Device{AppareilID: m.ID, NumeroCompteurAppareil: m.Serial, Date: day.Format("2006-01-02T15:04:05"), Fluide: fluid, ValeurIndex: value, Emplacement: row[1], Unite: unit}, true, nil
}
