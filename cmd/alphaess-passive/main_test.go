package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Heistergand/AlphaESS-to-MQTT_T10-HV/internal/config"
)

func TestParseFlagsConfig(t *testing.T) {
	opts, err := parseFlags([]string{"--config", "configs/example.yaml"})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}

	if opts.ConfigPath != "configs/example.yaml" {
		t.Fatalf("config path = %q", opts.ConfigPath)
	}
}

func TestParseFlagsRejectsFileAndInterface(t *testing.T) {
	if _, err := parseFlags([]string{"-f", "pcaps/sample.pcapng", "-i", "eth0"}); err == nil {
		t.Fatal("parseFlags() error = nil, want error")
	}
}

func TestParseFlagsRejectsRealtimeConflict(t *testing.T) {
	if _, err := parseFlags([]string{"--realtime", "--no-realtime"}); err == nil {
		t.Fatal("parseFlags() error = nil, want error")
	}
}

func TestParseFlagsRejectsMQTTConflict(t *testing.T) {
	if _, err := parseFlags([]string{"--mqtt", "--no-mqtt"}); err == nil {
		t.Fatal("parseFlags() error = nil, want error")
	}
}

func TestParseFlagsVerbose(t *testing.T) {
	opts, err := parseFlags([]string{"-v"})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if !opts.Verbose {
		t.Fatal("verbose = false, want true")
	}
}

func TestApplyInputOverridesKeepsConfiguredInterface(t *testing.T) {
	cfg := testConfig()

	if err := applyInputOverrides(&cfg, cliOptions{}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}

	if cfg.Input.Mode != config.InputModeInterface {
		t.Fatalf("mode = %q, want %q", cfg.Input.Mode, config.InputModeInterface)
	}
	if cfg.Input.Interface != "enx0c3796bef0d8" {
		t.Fatalf("interface = %q", cfg.Input.Interface)
	}
}

func TestApplyInputOverridesFile(t *testing.T) {
	cfg := testConfig()

	if err := applyInputOverrides(&cfg, cliOptions{PCAPFile: "pcaps/sample.pcapng", Realtime: true}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}

	if cfg.Input.Mode != config.InputModeFile {
		t.Fatalf("mode = %q, want %q", cfg.Input.Mode, config.InputModeFile)
	}
	if cfg.Input.File != "pcaps/sample.pcapng" {
		t.Fatalf("file = %q", cfg.Input.File)
	}
	if cfg.Input.Interface != "" {
		t.Fatalf("interface = %q, want empty", cfg.Input.Interface)
	}
	if !cfg.Simulation.Realtime {
		t.Fatal("simulation realtime = false, want true")
	}
}

func TestApplyInputOverridesInterface(t *testing.T) {
	cfg := testConfig()

	if err := applyInputOverrides(&cfg, cliOptions{Interface: "eth0"}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}

	if cfg.Input.Mode != config.InputModeInterface {
		t.Fatalf("mode = %q, want %q", cfg.Input.Mode, config.InputModeInterface)
	}
	if cfg.Input.Interface != "eth0" {
		t.Fatalf("input interface = %q", cfg.Input.Interface)
	}
	if cfg.Capture.Interface != "eth0" {
		t.Fatalf("capture interface = %q", cfg.Capture.Interface)
	}
}

func TestApplyInputOverridesVerbose(t *testing.T) {
	cfg := testConfig()

	if err := applyInputOverrides(&cfg, cliOptions{Verbose: true}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}

	if cfg.Logging.Level != "debug" {
		t.Fatalf("log level = %q, want debug", cfg.Logging.Level)
	}
	if !isVerbose(cfg) {
		t.Fatal("isVerbose() = false, want true")
	}
}

func TestApplyInputOverridesMQTT(t *testing.T) {
	cfg := testConfig()

	if err := applyInputOverrides(&cfg, cliOptions{MQTT: true}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}
	if !cfg.MQTT.Enabled {
		t.Fatal("mqtt enabled = false, want true")
	}

	if err := applyInputOverrides(&cfg, cliOptions{NoMQTT: true}); err != nil {
		t.Fatalf("applyInputOverrides() error = %v", err)
	}
	if cfg.MQTT.Enabled {
		t.Fatal("mqtt enabled = true, want false")
	}
}

func TestCompleteConfigInteractiveNoSave(t *testing.T) {
	var cfg config.Config
	input := bytes.NewBufferString(stringsJoinLines(
		"enx0c3796bef0d8",
		"192.0.2.10",
		"198.51.100.10",
		"7777",
		"tcp://127.0.0.1:1883",
		"alphaess",
		"homeassistant",
		"alphaess_t10_hv",
		"AlphaESS SMILE-T10-HV",
		"nein",
	))
	var output bytes.Buffer

	err := completeConfigInteractive(&cfg, "unused.yaml", false, input, &output)
	if err != nil {
		t.Fatalf("completeConfigInteractive() error = %v", err)
	}

	if cfg.Input.Mode != config.InputModeInterface {
		t.Fatalf("input mode = %q", cfg.Input.Mode)
	}
	if cfg.Input.Interface != "enx0c3796bef0d8" {
		t.Fatalf("input interface = %q", cfg.Input.Interface)
	}
	if cfg.Capture.CloudPort != 7777 {
		t.Fatalf("cloud port = %d", cfg.Capture.CloudPort)
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCompleteConfigInteractiveAbort(t *testing.T) {
	var cfg config.Config
	input := bytes.NewBufferString(stringsJoinLines(
		"pcaps/sample.pcapng",
		"192.0.2.10",
		"198.51.100.10",
		"7777",
		"tcp://127.0.0.1:1883",
		"alphaess",
		"homeassistant",
		"alphaess_t10_hv",
		"AlphaESS SMILE-T10-HV",
		"abbrechen",
	))
	var output bytes.Buffer

	err := completeConfigInteractive(&cfg, "unused.yaml", false, input, &output)
	if !errors.Is(err, errSetupAborted) {
		t.Fatalf("completeConfigInteractive() error = %v, want %v", err, errSetupAborted)
	}
}

func TestApplyInputSourceDetectsQuotedPCAPFile(t *testing.T) {
	var cfg config.Config

	applyInputSource(&cfg, `"/home/user/alphaess/pcaps/sample.pcapng"`)

	if cfg.Input.Mode != config.InputModeFile {
		t.Fatalf("input mode = %q, want %q", cfg.Input.Mode, config.InputModeFile)
	}
	if cfg.Input.File != "/home/user/alphaess/pcaps/sample.pcapng" {
		t.Fatalf("input file = %q", cfg.Input.File)
	}
	if cfg.Input.Interface != "" {
		t.Fatalf("input interface = %q, want empty", cfg.Input.Interface)
	}
}

func TestPublishOfflineStatus(t *testing.T) {
	publisher := &recordingPublisher{}

	if err := publishOfflineStatus(publisher, "alphaess"); err != nil {
		t.Fatalf("publishOfflineStatus() error = %v", err)
	}

	if publisher.topic != "alphaess/status" {
		t.Fatalf("topic = %q, want alphaess/status", publisher.topic)
	}
	if string(publisher.payload) != "offline" {
		t.Fatalf("payload = %q, want offline", publisher.payload)
	}
	if !publisher.retained {
		t.Fatal("retained = false, want true")
	}
}

func TestPublishOfflineStatusIgnoresNilPublisher(t *testing.T) {
	if err := publishOfflineStatus(nil, "alphaess"); err != nil {
		t.Fatalf("publishOfflineStatus(nil) error = %v", err)
	}
}

func TestLoadSensorMappingUsesBuiltInDefaultsWhenUnset(t *testing.T) {
	sensors, path, err := loadSensorMapping(testConfig(), "configs/example.yaml")
	if err != nil {
		t.Fatalf("loadSensorMapping() error = %v", err)
	}
	if sensors != nil {
		t.Fatalf("sensors = %v, want nil for built-in defaults", sensors)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}
}

func TestLoadSensorMappingLoadsRelativeFile(t *testing.T) {
	cfg := testConfig()
	cfg.HomeAssistant.MappingFile = "homeassistant-mapping.yaml"

	sensors, path, err := loadSensorMapping(cfg, "../../configs/example.yaml")
	if err != nil {
		t.Fatalf("loadSensorMapping() error = %v", err)
	}
	if path != "..\\..\\configs\\homeassistant-mapping.yaml" && path != "../../configs/homeassistant-mapping.yaml" {
		t.Fatalf("path = %q", path)
	}
	if len(sensors) == 0 {
		t.Fatal("sensors is empty")
	}
}

func testConfig() config.Config {
	return config.Config{
		Input: config.InputConfig{
			Mode:      config.InputModeInterface,
			Interface: "enx0c3796bef0d8",
		},
		Capture: config.CaptureConfig{
			Interface: "enx0c3796bef0d8",
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: config.MQTTConfig{
			Broker:    "tcp://127.0.0.1:1883",
			TopicBase: "alphaess",
		},
		HomeAssistant: config.HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
		},
	}
}

type recordingPublisher struct {
	topic    string
	payload  []byte
	retained bool
}

func (p *recordingPublisher) Connect(context.Context) error {
	return nil
}

func (p *recordingPublisher) PublishJSON(_ context.Context, topic string, payload []byte, retained bool) error {
	p.topic = topic
	p.payload = payload
	p.retained = retained
	return nil
}

func (p *recordingPublisher) Close() error {
	return nil
}

func stringsJoinLines(lines ...string) string {
	var b bytes.Buffer
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
