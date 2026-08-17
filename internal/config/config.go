package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const EnvConfigPath = "ALPHAESS_PASSIVE_CONFIG"

type Config struct {
	Input         InputConfig         `yaml:"input"`
	Simulation    SimulationConfig    `yaml:"simulation"`
	Capture       CaptureConfig       `yaml:"capture"`
	MQTT          MQTTConfig          `yaml:"mqtt"`
	HomeAssistant HomeAssistantConfig `yaml:"homeassistant"`
	Logging       LoggingConfig       `yaml:"logging"`
}

type InputMode string

const (
	InputModeInterface InputMode = "interface"
	InputModeFile      InputMode = "file"
	InterfaceAuto                = "auto"
)

type InputConfig struct {
	Mode      InputMode `yaml:"mode"`
	Interface string    `yaml:"interface"`
	File      string    `yaml:"file"`
}

type SimulationConfig struct {
	Realtime bool `yaml:"realtime"`
}

type CaptureConfig struct {
	Interface string `yaml:"interface"`
	SourceIP  string `yaml:"source_ip"`
	CloudIP   string `yaml:"cloud_ip"`
	CloudPort int    `yaml:"cloud_port"`
}

type MQTTConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Broker       string `yaml:"broker"`
	Username     string `yaml:"username"`
	PasswordFile string `yaml:"password_file"`
	TopicBase    string `yaml:"topic_base"`
}

type HomeAssistantConfig struct {
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	DeviceID        string `yaml:"device_id"`
	DeviceName      string `yaml:"device_name"`
	MappingFile     string `yaml:"mapping_file"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

func Load(path string) (Config, error) {
	cfg, err := LoadPartial(path)
	if err != nil {
		return Config{}, err
	}

	if err := cfg.Normalize(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func LoadPartial(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg = cfg.forSave()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (cfg Config) forSave() Config {
	cfg.Input.File = compactHomePath(cfg.Input.File)
	cfg.MQTT.PasswordFile = compactHomePath(cfg.MQTT.PasswordFile)
	cfg.HomeAssistant.MappingFile = compactHomePath(cfg.HomeAssistant.MappingFile)
	return cfg
}

func (cfg *Config) Normalize() error {
	cfg.Input.Interface = cleanValue(cfg.Input.Interface)
	cfg.Input.File = cleanPathValue(cfg.Input.File)
	cfg.Capture.Interface = cleanValue(cfg.Capture.Interface)
	cfg.MQTT.PasswordFile = cleanPathValue(cfg.MQTT.PasswordFile)
	cfg.HomeAssistant.MappingFile = cleanPathValue(cfg.HomeAssistant.MappingFile)

	if LooksLikeCaptureFile(cfg.Input.Interface) {
		cfg.Input.File = cfg.Input.Interface
		cfg.Input.Interface = ""
		cfg.Input.Mode = InputModeFile
	}

	if cfg.Input.Mode == "" {
		cfg.Input.Mode = InputModeInterface
	}

	switch cfg.Input.Mode {
	case InputModeInterface:
		if cfg.Input.Interface == "" {
			cfg.Input.Interface = cfg.Capture.Interface
		}
		if cfg.Capture.Interface == "" {
			cfg.Capture.Interface = cfg.Input.Interface
		}
	case InputModeFile:
	default:
		return fmt.Errorf("unsupported input mode %q", cfg.Input.Mode)
	}

	return nil
}

func LooksLikeCaptureFile(value string) bool {
	value = cleanValue(value)
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, ".pcap") || strings.HasSuffix(lower, ".pcapng") {
		return true
	}
	info, err := os.Stat(value)
	return err == nil && !info.IsDir()
}

func cleanValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func cleanPathValue(value string) string {
	return os.ExpandEnv(cleanValue(value))
}

func compactHomePath(value string) string {
	value = cleanValue(value)
	if value == "" || strings.Contains(value, "${HOME}") {
		return value
	}

	home := cleanValue(os.Getenv("HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return value
		}
		home = cleanValue(home)
	}
	if home == "" {
		return value
	}

	cleanHome := filepath.Clean(home)
	cleanPath := filepath.Clean(value)
	if cleanPath == cleanHome {
		return "${HOME}"
	}

	relative, err := filepath.Rel(cleanHome, cleanPath)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return value
	}
	return filepath.ToSlash(filepath.Join("${HOME}", relative))
}

func (cfg Config) Validate() error {
	var errs []error

	switch cfg.Input.Mode {
	case InputModeInterface:
		if cfg.Input.Interface == "" {
			errs = append(errs, errors.New("input.interface or capture.interface is required"))
		}
		if cfg.Simulation.Realtime {
			errs = append(errs, errors.New("simulation.realtime is only valid when input.mode is file"))
		}
	case InputModeFile:
		if cfg.Input.File == "" {
			errs = append(errs, errors.New("input.file is required when input.mode is file"))
		}
	default:
		errs = append(errs, fmt.Errorf("unsupported input mode %q", cfg.Input.Mode))
	}

	if cfg.Capture.SourceIP == "" {
		errs = append(errs, errors.New("capture.source_ip is required"))
	}
	if cfg.Capture.CloudIP == "" {
		errs = append(errs, errors.New("capture.cloud_ip is required"))
	}
	if cfg.Capture.CloudPort == 0 {
		errs = append(errs, errors.New("capture.cloud_port is required"))
	}
	if cfg.MQTT.Broker == "" {
		errs = append(errs, errors.New("mqtt.broker is required"))
	}
	if cfg.MQTT.TopicBase == "" {
		errs = append(errs, errors.New("mqtt.topic_base is required"))
	}
	if cfg.HomeAssistant.DiscoveryPrefix == "" {
		errs = append(errs, errors.New("homeassistant.discovery_prefix is required"))
	}
	if cfg.HomeAssistant.DeviceID == "" {
		errs = append(errs, errors.New("homeassistant.device_id is required"))
	}
	if cfg.HomeAssistant.DeviceName == "" {
		errs = append(errs, errors.New("homeassistant.device_name is required"))
	}

	return errors.Join(errs...)
}

func Locate(explicitPath string) (string, error) {
	path, _, err := Resolve(explicitPath)
	return path, err
}

func Resolve(explicitPath string) (path string, exists bool, err error) {
	if explicitPath != "" {
		exists, err := fileExists(explicitPath)
		return explicitPath, exists, err
	}
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		exists, err := fileExists(envPath)
		return envPath, exists, err
	}

	for _, candidate := range candidatePaths() {
		if candidate == "" {
			continue
		}
		exists, err := fileExists(candidate)
		if err != nil {
			return "", false, err
		}
		if exists {
			return candidate, true, nil
		}
	}

	return defaultWritePath(), false, nil
}

func candidatePaths() []string {
	paths := []string{}

	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		paths = append(paths, envPath)
	}
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		paths = append(paths, filepath.Join(xdgConfigHome, "alphaess-passive", "config.yaml"))
	}
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths, filepath.Join(home, ".config", "alphaess-passive", "config.yaml"))
	}
	paths = append(paths, "/etc/alphaess-passive/config.yaml")

	return paths
}

func defaultWritePath() string {
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "alphaess-passive", "config.yaml")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".config", "alphaess-passive", "config.yaml")
	}
	return "/etc/alphaess-passive/config.yaml"
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
