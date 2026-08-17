package homeassistant

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/Heistergand/AlphaESS-to-MQTT_T10-HV/internal/mqtt"
	"gopkg.in/yaml.v3"
)

type Device struct {
	ID           string
	Name         string
	Manufacturer string
	Model        string
}

type Sensor struct {
	Field       string `yaml:"field"`
	ObjectID    string `yaml:"object_id"`
	Name        string `yaml:"name"`
	DeviceClass string `yaml:"device_class"`
	StateClass  string `yaml:"state_class"`
	Unit        string `yaml:"unit"`
	Diagnostic  bool   `yaml:"diagnostic"`
}

type Mapping struct {
	IncludeDefaults bool     `yaml:"include_defaults"`
	Sensors         []Sensor `yaml:"sensors"`
}

type discoveryPayload struct {
	Name                string          `json:"name"`
	UniqueID            string          `json:"unique_id"`
	StateTopic          string          `json:"state_topic"`
	ValueTemplate       string          `json:"value_template"`
	DeviceClass         string          `json:"device_class,omitempty"`
	StateClass          string          `json:"state_class,omitempty"`
	UnitOfMeasurement   string          `json:"unit_of_measurement,omitempty"`
	SuggestedPrecision  *int            `json:"suggested_display_precision,omitempty"`
	EntityCategory      string          `json:"entity_category,omitempty"`
	EnabledByDefault    *bool           `json:"enabled_by_default,omitempty"`
	AvailabilityTopic   string          `json:"availability_topic,omitempty"`
	PayloadAvailable    string          `json:"payload_available,omitempty"`
	PayloadNotAvailable string          `json:"payload_not_available,omitempty"`
	Device              discoveryDevice `json:"device"`
}

type discoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
}

func BaseSensors() []Sensor {
	return []Sensor{
		{Field: "SOC", ObjectID: "soc", Name: "State of Charge", DeviceClass: "battery", StateClass: "measurement", Unit: "%"},
		{Field: "Pbat", ObjectID: "battery_power", Name: "Battery Power", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PpvTotal", ObjectID: "pv_power_total", Name: "PV Power Total", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "Ppv1", ObjectID: "pv_power_1", Name: "PV Power 1", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "Ppv2", ObjectID: "pv_power_2", Name: "PV Power 2", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "Ppv3", ObjectID: "pv_power_3", Name: "PV Power 3", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "Ppv4", ObjectID: "pv_power_4", Name: "PV Power 4", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "EpvTotal", ObjectID: "pv_energy_total", Name: "PV Energy Total", DeviceClass: "energy", StateClass: "total_increasing", Unit: "kWh"},
		{Field: "PrealL1", ObjectID: "inverter_power_l1", Name: "Inverter Power L1", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PrealL2", ObjectID: "inverter_power_l2", Name: "Inverter Power L2", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PrealL3", ObjectID: "inverter_power_l3", Name: "Inverter Power L3", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PrealTotal", ObjectID: "inverter_power_total", Name: "Inverter Power Total", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PmeterL1", ObjectID: "meter_power_l1", Name: "Meter Power L1", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PmeterL2", ObjectID: "meter_power_l2", Name: "Meter Power L2", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PmeterL3", ObjectID: "meter_power_l3", Name: "Meter Power L3", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "PmeterTotal", ObjectID: "grid_power_total", Name: "Grid Power Total", DeviceClass: "power", StateClass: "measurement", Unit: "W"},
		{Field: "Einput", ObjectID: "grid_import_energy_total", Name: "Grid Import Energy Total", DeviceClass: "energy", StateClass: "total_increasing", Unit: "kWh"},
		{Field: "Eoutput", ObjectID: "grid_export_energy_total", Name: "Grid Export Energy Total", DeviceClass: "energy", StateClass: "total_increasing", Unit: "kWh"},
		{Field: "Echarge", ObjectID: "battery_charge_energy_total", Name: "Battery Charge Energy Total", DeviceClass: "energy", StateClass: "total_increasing", Unit: "kWh"},
		{Field: "EDischarge", ObjectID: "battery_discharge_energy_total", Name: "Battery Discharge Energy Total", DeviceClass: "energy", StateClass: "total_increasing", Unit: "kWh"},
	}
}

func SensorForField(field string) Sensor {
	for _, sensor := range BaseSensors() {
		if sensor.Field == field {
			return sensor
		}
	}
	return Sensor{
		Field:      field,
		ObjectID:   "field_" + objectID(field),
		Name:       field,
		Diagnostic: true,
	}
}

func LoadMappingFile(path string) ([]Sensor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mapping Mapping
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, err
	}

	sensors := []Sensor{}
	if mapping.IncludeDefaults {
		sensors = append(sensors, BaseSensors()...)
	}
	sensors = append(sensors, mapping.Sensors...)

	seen := map[string]struct{}{}
	for index := range sensors {
		normalizeSensor(&sensors[index])
		if sensors[index].Field == "" {
			return nil, fmt.Errorf("sensor %d has no field", index)
		}
		if sensors[index].ObjectID == "" {
			return nil, fmt.Errorf("sensor %q has no object_id", sensors[index].Field)
		}
		if _, ok := seen[sensors[index].ObjectID]; ok {
			return nil, fmt.Errorf("duplicate object_id %q", sensors[index].ObjectID)
		}
		seen[sensors[index].ObjectID] = struct{}{}
	}

	return sensors, nil
}

func normalizeSensor(sensor *Sensor) {
	if sensor.ObjectID == "" && sensor.Field != "" {
		sensor.ObjectID = objectID(sensor.Field)
	}
	if sensor.Name == "" {
		sensor.Name = sensor.Field
	}
}

func DiscoveryMessages(discoveryPrefix, topicBase string, device Device, sensors []Sensor) ([]mqtt.Message, error) {
	if device.Manufacturer == "" {
		device.Manufacturer = "AlphaESS"
	}
	if device.Model == "" {
		device.Model = "SMILE-T10-HV"
	}

	messages := make([]mqtt.Message, 0, len(sensors))
	stateTopic := topicBase + "/raw/state"
	availabilityTopic := topicBase + "/status"
	for _, sensor := range sensors {
		precision := suggestedPrecision(sensor)
		enabledByDefault := enabledByDefault(sensor)
		payload := discoveryPayload{
			Name:                sensor.Name,
			UniqueID:            device.ID + "_" + sensor.ObjectID,
			StateTopic:          stateTopic,
			ValueTemplate:       valueTemplate(sensor),
			DeviceClass:         sensor.DeviceClass,
			StateClass:          sensor.StateClass,
			UnitOfMeasurement:   sensor.Unit,
			SuggestedPrecision:  precision,
			EntityCategory:      entityCategory(sensor),
			EnabledByDefault:    enabledByDefault,
			AvailabilityTopic:   availabilityTopic,
			PayloadAvailable:    "online",
			PayloadNotAvailable: "offline",
			Device: discoveryDevice{
				Identifiers:  []string{device.ID},
				Name:         device.Name,
				Manufacturer: device.Manufacturer,
				Model:        device.Model,
			},
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		topic := strings.TrimRight(discoveryPrefix, "/") + "/sensor/" + device.ID + "/" + sensor.ObjectID + "/config"
		messages = append(messages, mqtt.Message{
			Topic:    topic,
			Payload:  data,
			Retained: true,
		})
	}

	return messages, nil
}

func valueTemplate(sensor Sensor) string {
	if sensor.Diagnostic {
		return "{{ value_json['values'].get('" + sensor.Field + "', value_json['fields'].get('" + sensor.Field + "')) }}"
	}
	return "{{ value_json['values'].get('" + sensor.Field + "') }}"
}

func entityCategory(sensor Sensor) string {
	if sensor.Diagnostic {
		return "diagnostic"
	}
	return ""
}

func enabledByDefault(sensor Sensor) *bool {
	if sensor.Diagnostic {
		value := false
		return &value
	}
	return nil
}

func suggestedPrecision(sensor Sensor) *int {
	if sensor.Field == "SOC" {
		precision := 1
		return &precision
	}
	return nil
}

func objectID(value string) string {
	var builder strings.Builder
	var previousUnderscore bool
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) && builder.Len() > 0 && !previousUnderscore {
				builder.WriteByte('_')
			}
			builder.WriteRune(unicode.ToLower(r))
			previousUnderscore = false
		default:
			if builder.Len() > 0 && !previousUnderscore {
				builder.WriteByte('_')
				previousUnderscore = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}
