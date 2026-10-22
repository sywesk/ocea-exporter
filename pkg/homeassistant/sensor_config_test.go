package homeassistant

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

func TestDiscoveryCompatibility(t *testing.T) {
	for _, tc := range []struct{ fluid, name, unit string }{{"EauFroide", "water_meter", "m³"}, {"EauChaude", "hot_water_meter", "m³"}, {"Cetc", "heating_energy_meter", "kWh"}} {
		t.Run(tc.fluid, func(t *testing.T) {
			topics, err := buildSensorTopics(oceaapi.FluidName(tc.fluid), "12345")
			if err != nil {
				t.Fatal(err)
			}
			base := "homeassistant/sensor/ocea_exporter/" + tc.name + "_12345"
			if topics.Config != base+"/config" || topics.State != base+"/state" {
				t.Fatalf("topics changed: %+v", topics)
			}
			cfg, err := getFluidSensorConfig(oceaapi.FluidName(tc.fluid), "12345", topics.State)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]interface{}
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["unique_id"] != "12345_meter" || fields["name"] != tc.name || fields["state_topic"] != topics.State || fields["state_class"] != "total" || fields["unit_of_measurement"] != tc.unit {
				t.Fatalf("discovery compatibility broken: %s", data)
			}
		})
	}
	if _, err := buildSensorTopics("unknown", "serial"); !errors.Is(err, ErrUnknownFluid) {
		t.Fatal("unknown fluid accepted")
	}
}
