package main

import (
	"fmt"
	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"log"
)

type Counter struct {
	MonthToDate float64
	YearToDate  float64
}

func (c Counter) String() string {
	return fmt.Sprintf("MTD=%f YTD=%f", c.MonthToDate, c.YearToDate)
}

type Counters map[string]Counter

func fetchCounters(client oceaapi.APIClient) (Counters, error) {
	counters := Counters{}

	resident, err := client.GetResident()
	if err != nil {
		return Counters{}, fmt.Errorf("failed to get resident: %w", err)
	}

	log.Printf("fetch: resident %s %s (id %s)", resident.Resident.Nom, resident.Resident.Prenom, resident.Resident.ID)

	if len(resident.Occupations) == 0 {
		return Counters{}, fmt.Errorf("no occupation found")
	}

	localID := resident.Occupations[0].LogementID
	log.Printf("fetch: found local %s", localID)

	devices, err := client.GetDevices(localID)
	if err != nil {
		return Counters{}, fmt.Errorf("failed to get devices for local %s: %w", localID, err)
	}

	for _, device := range devices {
		log.Printf("device: serial=%s fluid=%s value=%f", device.NumeroCompteurAppareil, device.Fluide, device.ValeurIndex)
	}

	local, err := client.GetLocal(localID)
	if err != nil {
		return Counters{}, fmt.Errorf("failed to get local %s: %w", localID, err)
	}
	if len(local.FluidesRestitues) == 0 {
		return Counters{}, fmt.Errorf("no fluid found for local %s", localID)
	}

	for _, fluid := range local.FluidesRestitues {
		dashboard, err := client.GetFluidDashboard(localID, fluid.Fluide)
		if err != nil {
			return Counters{}, fmt.Errorf("failed to get dashboard for local %s and fluid %s: %w", localID, fluid.Fluide, err)
		}

		counters[fluid.Fluide] = Counter{
			MonthToDate: dashboard.ConsoMoisCourant,
			YearToDate:  dashboard.ConsoCumuleeAnneeCourante,
		}

		log.Printf("fetch: got fluid %s: %s", fluid.Fluide, counters[fluid.Fluide])
	}

	return counters, nil
}
