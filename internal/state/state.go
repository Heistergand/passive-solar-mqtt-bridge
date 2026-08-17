package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Payload struct {
	DeviceID   string         `json:"device_id,omitempty"`
	DeviceName string         `json:"device_name,omitempty"`
	SN         string         `json:"sn,omitempty"`
	Time       string         `json:"time,omitempty"`
	Fields     map[string]any `json:"fields"`
	Values     map[string]any `json:"values,omitempty"`
}

func Build(fields map[string]any, configuredDeviceID, configuredDeviceName string) Payload {
	copiedFields := copyMap(fields)
	values := numericValues(copiedFields)
	addDerivedValues(copiedFields, values)

	sn := stringValue(copiedFields["SN"])
	deviceID := configuredDeviceID
	if deviceID == "" && sn != "" {
		deviceID = sanitizeID("alphaess_" + sn)
	}

	deviceName := configuredDeviceName
	if deviceName == "" && sn != "" {
		deviceName = "AlphaESS " + sn
	}

	return Payload{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		SN:         sn,
		Time:       stringValue(copiedFields["Time"]),
		Fields:     copiedFields,
		Values:     values,
	}
}

func Marshal(fields map[string]any, configuredDeviceID, configuredDeviceName string) ([]byte, error) {
	payload := Build(fields, configuredDeviceID, configuredDeviceName)
	return json.Marshal(payload)
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func numericValues(fields map[string]any) map[string]any {
	values := map[string]any{}
	for key, value := range fields {
		number, ok := parseNumber(value)
		if ok {
			values[key] = number
		}
	}
	return values
}

func addDerivedValues(fields map[string]any, values map[string]any) {
	addNumberedTotal(fields, values, "Ppv", "PpvTotal")
	addNumberedTotal(fields, values, "PrealL", "PrealTotal")
	addNumberedTotal(fields, values, "PmeterL", "PmeterTotal")
}

func addNumberedTotal(fields map[string]any, values map[string]any, prefix, totalKey string) {
	keys := matchingNumberedKeys(fields, prefix)
	if len(keys) > 0 {
		var total float64
		for _, key := range keys {
			number, ok := parseNumber(fields[key])
			if ok {
				total += number
			}
		}
		values[totalKey] = total
	}
}

func matchingNumberedKeys(fields map[string]any, prefix string) []string {
	keys := []string{}
	for key := range fields {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(key, prefix)
		if suffix == "" {
			continue
		}
		if _, err := strconv.Atoi(suffix); err == nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func parseNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		if typed == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func sanitizeID(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func NowPayload(fields map[string]any, configuredDeviceID, configuredDeviceName string) Payload {
	payload := Build(fields, configuredDeviceID, configuredDeviceName)
	if payload.Time == "" {
		payload.Time = time.Now().Format(time.RFC3339)
	}
	return payload
}
