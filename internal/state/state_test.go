package state

import (
	"encoding/json"
	"testing"
)

func TestBuildPreservesFieldsAndAddsNumericValues(t *testing.T) {
	payload := Build(map[string]any{
		"SN":   "ALB000000000000",
		"SOC":  "33.2",
		"Pbat": "600",
	}, "", "")

	if payload.DeviceID != "alphaess_alb000000000000" {
		t.Fatalf("device id = %q", payload.DeviceID)
	}
	if payload.SN != "ALB000000000000" {
		t.Fatalf("sn = %q", payload.SN)
	}
	if payload.Fields["SOC"] != "33.2" {
		t.Fatalf("raw SOC = %v", payload.Fields["SOC"])
	}
	if payload.Values["SOC"] != 33.2 {
		t.Fatalf("numeric SOC = %v", payload.Values["SOC"])
	}
}

func TestBuildAddsPVTotalFromAllPresentPVInputs(t *testing.T) {
	payload := Build(map[string]any{
		"Ppv1": "10",
		"Ppv2": "20",
		"Ppv3": "30",
		"Ppv4": "40",
	}, "device", "name")

	if payload.Values["PpvTotal"] != 100.0 {
		t.Fatalf("PpvTotal = %v", payload.Values["PpvTotal"])
	}
}

func TestBuildAddsPhaseTotals(t *testing.T) {
	payload := Build(map[string]any{
		"PrealL1":  "100",
		"PrealL2":  "200",
		"PrealL3":  "300",
		"PmeterL1": "-10",
		"PmeterL2": "-20",
		"PmeterL3": "-30",
	}, "device", "name")

	if payload.Values["PrealTotal"] != 600.0 {
		t.Fatalf("PrealTotal = %v", payload.Values["PrealTotal"])
	}
	if payload.Values["PmeterTotal"] != -60.0 {
		t.Fatalf("PmeterTotal = %v", payload.Values["PmeterTotal"])
	}
}

func TestMarshalProducesJSON(t *testing.T) {
	data, err := Marshal(map[string]any{
		"SN":  "ALB000000000000",
		"SOC": "33.2",
	}, "alphaess_t10_hv", "AlphaESS SMILE-T10-HV")
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if decoded["device_id"] != "alphaess_t10_hv" {
		t.Fatalf("device_id = %v", decoded["device_id"])
	}
}

func TestBuildUsesConfiguredDeviceValues(t *testing.T) {
	payload := Build(map[string]any{
		"SN": "ALB000000000000",
	}, "configured_id", "Configured Name")

	if payload.DeviceID != "configured_id" {
		t.Fatalf("device id = %q", payload.DeviceID)
	}
	if payload.DeviceName != "Configured Name" {
		t.Fatalf("device name = %q", payload.DeviceName)
	}
}
