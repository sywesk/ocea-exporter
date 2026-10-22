package homeassistant

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

const manufacturerName = "Ocea"

type StateClass string

const (
	TotalStateClass StateClass = "total"
)

type DeviceClass string

const (
	EnergyDeviceClass DeviceClass = "energy"
	WaterDeviceClass  DeviceClass = "water"
)

type Icon string

const (
	WaterIcon            Icon = "mdi:water"
	WaterThermometerIcon Icon = "mdi:water-thermometer"
	RadiatorIcon         Icon = "mdi:radiator"
)

type Unit string

const (
	CubicMeterUnit   Unit = "m³"
	KiloWattHourUnit Unit = "kWh"
)

type FluidDescription struct {
	Unit        Unit
	DeviceClass DeviceClass
	Icon        Icon
	Name        string
}

var fluidDescriptions = map[oceaapi.FluidName]FluidDescription{
	oceaapi.FluidHeating: {
		Unit:        KiloWattHourUnit,
		DeviceClass: EnergyDeviceClass,
		Icon:        RadiatorIcon,
		Name:        "heating_energy_meter",
	},
	oceaapi.FluidColdWater: {
		Unit:        CubicMeterUnit,
		DeviceClass: WaterDeviceClass,
		Icon:        WaterIcon,
		Name:        "water_meter",
	},
	oceaapi.FluidHotWater: {
		Unit:        CubicMeterUnit,
		DeviceClass: WaterDeviceClass,
		Icon:        WaterThermometerIcon,
		Name:        "hot_water_meter",
	},
}

var ErrUnknownFluid = errors.New("unknown fluid")

type DeviceConfig struct {
	Identifiers  []string `json:"identifiers"`
	Manufacturer string   `json:"manufacturer"`
	Name         string   `json:"name"`
}

type SensorConfig struct {
	DeviceClass       DeviceClass  `json:"device_class"`
	EnabledByDefault  bool         `json:"enabled_by_default"`
	Icon              Icon         `json:"icon"`
	Name              string       `json:"name"`
	StateClass        StateClass   `json:"state_class"`
	UnitOfMeasurement Unit         `json:"unit_of_measurement"`
	StateTopic        string       `json:"state_topic"`
	UniqueID          string       `json:"unique_id"`
	Device            DeviceConfig `json:"device"`
}

func getFluidSensorConfig(fluid oceaapi.FluidName, serial string, stateTopic string) (SensorConfig, error) {
	desc, ok := fluidDescriptions[fluid]
	if !ok {
		return SensorConfig{}, ErrUnknownFluid
	}

	return SensorConfig{
		DeviceClass:       desc.DeviceClass,
		Name:              desc.Name,
		EnabledByDefault:  true,
		Icon:              desc.Icon,
		StateClass:        TotalStateClass,
		UnitOfMeasurement: desc.Unit,
		StateTopic:        stateTopic,
		UniqueID:          fmt.Sprintf("%s_meter", serial),
		Device: DeviceConfig{
			Identifiers: []string{
				serial,
			},
			Manufacturer: manufacturerName,
			Name:         fmt.Sprintf("%s %s", fluid, serial),
		},
	}, nil
}

type SensorTopics struct {
	Config string
	State  string
}

func buildSensorTopics(fluid oceaapi.FluidName, serial string) (SensorTopics, error) {
	if serial == "" || strings.ContainsAny(serial, "/#+\x00") {
		return SensorTopics{}, fmt.Errorf("invalid meter serial %q", serial)
	}
	desc, ok := fluidDescriptions[fluid]
	if !ok {
		return SensorTopics{}, ErrUnknownFluid
	}

	baseTopic := fmt.Sprintf("homeassistant/sensor/ocea_exporter/%s_%s", desc.Name, serial)

	return SensorTopics{
		Config: baseTopic + "/config",
		State:  baseTopic + "/state",
	}, nil
}

// buildOldSensorTopics builds the previous MQTT topics that were removed, just to be able to publish an empty packet to
// remove the previous sensors from homeassistant auto-discovery.
func buildOldSensorTopics(fluid oceaapi.FluidName) SensorTopics {
	desc := fluidDescriptions[fluid]
	baseTopic := fmt.Sprintf("homeassistant/sensor/ocea_exporter/%s", desc.Name)
	return SensorTopics{
		Config: baseTopic + "/config",
		State:  baseTopic + "/state",
	}
}
