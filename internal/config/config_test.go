package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExampleConfig(t *testing.T) {
	cfg, err := Load("../../configs/example.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Input.Mode != InputModeInterface {
		t.Fatalf("input mode = %q, want %q", cfg.Input.Mode, InputModeInterface)
	}
	if cfg.Input.Interface != InterfaceAuto {
		t.Fatalf("input interface = %q", cfg.Input.Interface)
	}
	if cfg.Capture.SourceIP != "192.0.2.10" {
		t.Fatalf("source ip = %q", cfg.Capture.SourceIP)
	}
	if cfg.MQTT.TopicBase != "alphaess" {
		t.Fatalf("topic base = %q", cfg.MQTT.TopicBase)
	}
	if cfg.HomeAssistant.MappingFile != "" {
		t.Fatalf("mapping file = %q, want empty default", cfg.HomeAssistant.MappingFile)
	}
}

func TestNormalizeDerivesInputInterfaceFromCapture(t *testing.T) {
	cfg := Config{
		Capture: CaptureConfig{
			Interface: "eth0",
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: MQTTConfig{
			Broker:    "tcp://127.0.0.1:1883",
			TopicBase: "alphaess",
		},
		HomeAssistant: HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
		},
	}

	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.Input.Mode != InputModeInterface {
		t.Fatalf("input mode = %q, want %q", cfg.Input.Mode, InputModeInterface)
	}
	if cfg.Input.Interface != "eth0" {
		t.Fatalf("input interface = %q", cfg.Input.Interface)
	}
}

func TestNormalizeConvertsQuotedInterfacePCAPToFileInput(t *testing.T) {
	cfg := Config{
		Input: InputConfig{
			Mode:      InputModeInterface,
			Interface: `"/home/user/alphaess/pcaps/sample.pcapng"`,
		},
		Capture: CaptureConfig{
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: MQTTConfig{
			Broker:    "tcp://127.0.0.1:1883",
			TopicBase: "alphaess",
		},
		HomeAssistant: HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
		},
	}

	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.Input.Mode != InputModeFile {
		t.Fatalf("input mode = %q, want %q", cfg.Input.Mode, InputModeFile)
	}
	if cfg.Input.File != "/home/user/alphaess/pcaps/sample.pcapng" {
		t.Fatalf("input file = %q", cfg.Input.File)
	}
	if cfg.Input.Interface != "" {
		t.Fatalf("input interface = %q, want empty", cfg.Input.Interface)
	}
}

func TestNormalizeExpandsEnvironmentVariablesInPathFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Config{
		Input: InputConfig{
			Mode: InputModeFile,
			File: `"${HOME}/pcaps/sample.pcapng"`,
		},
		Capture: CaptureConfig{
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: MQTTConfig{
			Broker:       "tcp://127.0.0.1:1883",
			PasswordFile: "${HOME}/.config/alphaess-passive/mqtt-password",
			TopicBase:    "alphaess",
		},
		HomeAssistant: HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
			MappingFile:     "${HOME}/.config/alphaess-passive/homeassistant-mapping.yaml",
		},
	}

	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if filepath.Clean(cfg.Input.File) != filepath.Join(home, "pcaps", "sample.pcapng") {
		t.Fatalf("input file = %q", cfg.Input.File)
	}
	if filepath.Clean(cfg.MQTT.PasswordFile) != filepath.Join(home, ".config", "alphaess-passive", "mqtt-password") {
		t.Fatalf("password file = %q", cfg.MQTT.PasswordFile)
	}
	if filepath.Clean(cfg.HomeAssistant.MappingFile) != filepath.Join(home, ".config", "alphaess-passive", "homeassistant-mapping.yaml") {
		t.Fatalf("mapping file = %q", cfg.HomeAssistant.MappingFile)
	}
}

func TestCompactHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "alphaess-passive", "mqtt-password")
	if got := compactHomePath(path); got != "${HOME}/.config/alphaess-passive/mqtt-password" {
		t.Fatalf("compactHomePath() = %q", got)
	}
	if got := compactHomePath("/etc/alphaess-passive/config.yaml"); got != "/etc/alphaess-passive/config.yaml" {
		t.Fatalf("compactHomePath(/etc) = %q", got)
	}
}

func TestSaveCompactsHomePathFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Config{
		Input: InputConfig{
			Mode: InputModeFile,
			File: filepath.Join(home, "pcaps", "sample.pcapng"),
		},
		Capture: CaptureConfig{
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: MQTTConfig{
			Broker:       "tcp://127.0.0.1:1883",
			PasswordFile: filepath.Join(home, ".config", "alphaess-passive", "mqtt-password"),
			TopicBase:    "alphaess",
		},
		HomeAssistant: HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
			MappingFile:     filepath.Join(home, ".config", "alphaess-passive", "homeassistant-mapping.yaml"),
		},
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"file: ${HOME}/pcaps/sample.pcapng",
		"password_file: ${HOME}/.config/alphaess-passive/mqtt-password",
		"mapping_file: ${HOME}/.config/alphaess-passive/homeassistant-mapping.yaml",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config does not contain %q:\n%s", want, text)
		}
	}
}

func TestValidateRejectsMissingOperationalConfig(t *testing.T) {
	var cfg Config

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestValidateRejectsRealtimeSimulationForInterfaceInput(t *testing.T) {
	cfg := Config{
		Input: InputConfig{
			Mode:      InputModeInterface,
			Interface: "eth0",
		},
		Simulation: SimulationConfig{
			Realtime: true,
		},
		Capture: CaptureConfig{
			Interface: "eth0",
			SourceIP:  "192.0.2.10",
			CloudIP:   "198.51.100.10",
			CloudPort: 7777,
		},
		MQTT: MQTTConfig{
			Broker:    "tcp://127.0.0.1:1883",
			TopicBase: "alphaess",
		},
		HomeAssistant: HomeAssistantConfig{
			DiscoveryPrefix: "homeassistant",
			DeviceID:        "alphaess_t10_hv",
			DeviceName:      "AlphaESS SMILE-T10-HV",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}
