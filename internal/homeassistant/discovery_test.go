package homeassistant

import (
	"encoding/json"
	"testing"
)

func TestDiscoveryMessages(t *testing.T) {
	messages, err := DiscoveryMessages("homeassistant", "alphaess", Device{
		ID:   "alphaess_t10_hv",
		Name: "AlphaESS SMILE-T10-HV",
	}, []Sensor{
		{Field: "SOC", ObjectID: "soc", Name: "State of Charge", DeviceClass: "battery", StateClass: "measurement", Unit: "%"},
	})
	if err != nil {
		t.Fatalf("DiscoveryMessages() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	message := messages[0]
	if message.Topic != "homeassistant/sensor/alphaess_t10_hv/soc/config" {
		t.Fatalf("topic = %q", message.Topic)
	}
	if !message.Retained {
		t.Fatal("retained = false, want true")
	}

	var payload map[string]any
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["state_topic"] != "alphaess/raw/state" {
		t.Fatalf("state_topic = %v", payload["state_topic"])
	}
	if payload["value_template"] != "{{ value_json['values'].get('SOC') }}" {
		t.Fatalf("value_template = %v", payload["value_template"])
	}
	if payload["device_class"] != "battery" {
		t.Fatalf("device_class = %v", payload["device_class"])
	}
}

func TestBaseSensorsIncludesDynamicPVTotal(t *testing.T) {
	for _, sensor := range BaseSensors() {
		if sensor.Field == "PpvTotal" {
			return
		}
	}
	t.Fatal("BaseSensors() does not include PpvTotal")
}

func TestBaseSensorsIncludesEnergyDashboardSensors(t *testing.T) {
	want := map[string]string{
		"EpvTotal":   "total_increasing",
		"Einput":     "total_increasing",
		"Eoutput":    "total_increasing",
		"Echarge":    "total_increasing",
		"EDischarge": "total_increasing",
	}
	for _, sensor := range BaseSensors() {
		stateClass, ok := want[sensor.Field]
		if !ok {
			continue
		}
		if sensor.DeviceClass != "energy" {
			t.Fatalf("%s device_class = %q", sensor.Field, sensor.DeviceClass)
		}
		if sensor.StateClass != stateClass {
			t.Fatalf("%s state_class = %q", sensor.Field, sensor.StateClass)
		}
		if sensor.Unit != "kWh" {
			t.Fatalf("%s unit = %q", sensor.Field, sensor.Unit)
		}
		delete(want, sensor.Field)
	}
	if len(want) > 0 {
		t.Fatalf("missing energy sensors: %v", want)
	}
}

func TestLoadMappingFile(t *testing.T) {
	sensors, err := LoadMappingFile("../../configs/homeassistant-mapping.yaml")
	if err != nil {
		t.Fatalf("LoadMappingFile() error = %v", err)
	}
	if len(sensors) == 0 {
		t.Fatal("sensors is empty")
	}
	if sensors[0].Field != "SOC" {
		t.Fatalf("first field = %q, want SOC", sensors[0].Field)
	}
}

func TestDiagnosticSensorDiscoveryIsDisabledByDefault(t *testing.T) {
	messages, err := DiscoveryMessages("homeassistant", "alphaess", Device{
		ID:   "alphaess_t10_hv",
		Name: "AlphaESS SMILE-T10-HV",
	}, []Sensor{
		SensorForField("WarInv"),
	})
	if err != nil {
		t.Fatalf("DiscoveryMessages() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(messages[0].Payload, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["entity_category"] != "diagnostic" {
		t.Fatalf("entity_category = %v", payload["entity_category"])
	}
	if payload["enabled_by_default"] != false {
		t.Fatalf("enabled_by_default = %v", payload["enabled_by_default"])
	}
}
