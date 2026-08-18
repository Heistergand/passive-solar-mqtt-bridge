package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/capture"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/config"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/homeassistant"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/mqtt"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	configPath, configExists, err := config.Resolve(opts.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	var cfg config.Config
	if configExists {
		cfg, err = config.LoadPartial(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			os.Exit(2)
		}
	}
	if err := applyInputOverrides(&cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}
	if err := completeConfigInteractive(&cfg, configPath, configExists, os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, errSetupAborted) {
			fmt.Fprintln(os.Stderr, "configuration aborted")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}
	if err := cfg.Normalize(); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}
	sensors, mappingPath, err := loadSensorMapping(cfg, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("passive-solar-mqtt starting")
	fmt.Printf("config: %s\n", configPath)
	if mappingPath != "" {
		fmt.Printf("home assistant mapping: %s\n", mappingPath)
	} else {
		fmt.Println("home assistant mapping: built-in defaults")
	}
	switch cfg.Input.Mode {
	case config.InputModeFile:
		fmt.Printf("input: pcap file %s\n", cfg.Input.File)
		if cfg.Simulation.Realtime {
			fmt.Println("simulation: realtime pcap timing")
		} else {
			fmt.Println("simulation: read pcap as fast as possible")
		}
	case config.InputModeInterface:
		fmt.Printf("input: live interface %s\n", cfg.Input.Interface)
	}
	fmt.Printf("alphaess endpoint: %s -> %s:%d\n", cfg.Capture.SourceIP, cfg.Capture.CloudIP, cfg.Capture.CloudPort)
	fmt.Printf("mqtt broker: %s\n", cfg.MQTT.Broker)
	if cfg.MQTT.Enabled {
		fmt.Println("mqtt publishing: enabled")
	} else {
		fmt.Println("mqtt publishing: disabled")
	}
	fmt.Println("mode: passive capture only")
	if isVerbose(cfg) {
		fmt.Printf("verbose: enabled (log level %s)\n", cfg.Logging.Level)
	}

	if err := run(ctx, cfg, sensors, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("passive-solar-mqtt stopped")
}

type cliOptions struct {
	ConfigPath string
	PCAPFile   string
	Interface  string
	Realtime   bool
	NoRealtime bool
	Verbose    bool
	MQTT       bool
	NoMQTT     bool
}

func parseFlags(args []string) (cliOptions, error) {
	flags := flag.NewFlagSet("passive-solar-mqtt", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var opts cliOptions
	flags.StringVar(&opts.ConfigPath, "config", "", "read configuration from this file")
	flags.StringVar(&opts.ConfigPath, "c", "", "read configuration from this file")
	flags.StringVar(&opts.PCAPFile, "file", "", "read packets from a pcap/pcapng file")
	flags.StringVar(&opts.PCAPFile, "f", "", "read packets from a pcap/pcapng file")
	flags.StringVar(&opts.Interface, "interface", "", "capture packets from a network interface")
	flags.StringVar(&opts.Interface, "i", "", "capture packets from a network interface")
	flags.BoolVar(&opts.Realtime, "realtime", false, "simulate pcap packet timing while reading from a file")
	flags.BoolVar(&opts.NoRealtime, "no-realtime", false, "read pcap files as fast as possible")
	flags.BoolVar(&opts.Verbose, "verbose", false, "enable verbose logging")
	flags.BoolVar(&opts.Verbose, "v", false, "enable verbose logging")
	flags.BoolVar(&opts.MQTT, "mqtt", false, "enable MQTT publishing")
	flags.BoolVar(&opts.NoMQTT, "no-mqtt", false, "disable MQTT publishing")

	if err := flags.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if flags.NArg() > 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if opts.PCAPFile != "" && opts.Interface != "" {
		return cliOptions{}, errors.New("use either --file/-f or --interface/-i, not both")
	}
	if opts.Realtime && opts.NoRealtime {
		return cliOptions{}, errors.New("use either --realtime or --no-realtime, not both")
	}
	if opts.MQTT && opts.NoMQTT {
		return cliOptions{}, errors.New("use either --mqtt or --no-mqtt, not both")
	}

	return opts, nil
}

func applyInputOverrides(cfg *config.Config, opts cliOptions) error {
	if opts.PCAPFile != "" && opts.Interface != "" {
		return errors.New("use either --file/-f or --interface/-i, not both")
	}
	if opts.PCAPFile != "" {
		cfg.Input.Mode = config.InputModeFile
		cfg.Input.File = opts.PCAPFile
		cfg.Input.Interface = ""
	}
	if opts.Interface != "" {
		cfg.Input.Mode = config.InputModeInterface
		cfg.Input.Interface = opts.Interface
		cfg.Capture.Interface = opts.Interface
	}
	if opts.Realtime {
		cfg.Simulation.Realtime = true
	}
	if opts.NoRealtime {
		cfg.Simulation.Realtime = false
	}
	if opts.Verbose {
		cfg.Logging.Level = "debug"
	}
	if opts.MQTT {
		cfg.MQTT.Enabled = true
	}
	if opts.NoMQTT {
		cfg.MQTT.Enabled = false
	}
	return nil
}

func run(ctx context.Context, cfg config.Config, sensors []homeassistant.Sensor, out io.Writer) error {
	var publisher mqtt.Publisher
	var client *mqtt.Client
	if cfg.MQTT.Enabled {
		clientID := cfg.HomeAssistant.DeviceID
		if clientID == "" {
			clientID = "passive-solar-mqtt"
		}
		client = mqtt.NewClient(mqtt.Options{
			Broker:       cfg.MQTT.Broker,
			ClientID:     clientID,
			Username:     cfg.MQTT.Username,
			PasswordFile: cfg.MQTT.PasswordFile,
			WillTopic:    cfg.MQTT.TopicBase + "/status",
			WillPayload:  []byte("offline"),
			WillRetained: true,
		})
		if err := client.Connect(ctx); err != nil {
			return err
		}
		defer client.Close()
		publisher = client
	}

	if cfg.Input.Mode == config.InputModeFile {
		summary, err := capture.ReadFile(ctx, capture.FileOptions{
			Path:            cfg.Input.File,
			SourceIP:        cfg.Capture.SourceIP,
			CloudIP:         cfg.Capture.CloudIP,
			CloudPort:       uint16(cfg.Capture.CloudPort),
			Realtime:        cfg.Simulation.Realtime,
			Verbose:         isVerbose(cfg),
			Writer:          out,
			TopicBase:       cfg.MQTT.TopicBase,
			DeviceID:        cfg.HomeAssistant.DeviceID,
			DeviceName:      cfg.HomeAssistant.DeviceName,
			DiscoveryPrefix: cfg.HomeAssistant.DiscoveryPrefix,
			Sensors:         sensors,
			Publisher:       publisher,
		})
		if err != nil {
			return err
		}
		printSummary(out, "pcap summary", summary)
		return nil
	}

	summary, err := capture.ReadLive(ctx, capture.LiveOptions{
		Interface:       cfg.Input.Interface,
		SourceIP:        cfg.Capture.SourceIP,
		CloudIP:         cfg.Capture.CloudIP,
		CloudPort:       uint16(cfg.Capture.CloudPort),
		Verbose:         isVerbose(cfg),
		Writer:          out,
		TopicBase:       cfg.MQTT.TopicBase,
		DeviceID:        cfg.HomeAssistant.DeviceID,
		DeviceName:      cfg.HomeAssistant.DeviceName,
		DiscoveryPrefix: cfg.HomeAssistant.DiscoveryPrefix,
		Sensors:         sensors,
		Publisher:       publisher,
	})
	if err != nil {
		return err
	}
	if err := publishOfflineStatus(publisher, cfg.MQTT.TopicBase); err != nil && isVerbose(cfg) {
		fmt.Fprintf(out, "warning: could not publish offline status: %v\n", err)
	}
	printSummary(out, "live capture summary", summary)
	return nil
}

func loadSensorMapping(cfg config.Config, configPath string) ([]homeassistant.Sensor, string, error) {
	if cfg.HomeAssistant.MappingFile == "" {
		return nil, "", nil
	}
	path := cfg.HomeAssistant.MappingFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(configPath), path)
	}
	sensors, err := homeassistant.LoadMappingFile(path)
	if err != nil {
		return nil, path, err
	}
	return sensors, path, nil
}

func publishOfflineStatus(publisher mqtt.Publisher, topicBase string) error {
	if publisher == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	message := mqtt.StatusMessage(topicBase, "offline")
	return publisher.PublishJSON(ctx, message.Topic, message.Payload, message.Retained)
}

func printSummary(out io.Writer, title string, summary capture.FileSummary) {
	fmt.Fprintf(out, "%s:\n", title)
	fmt.Fprintf(out, "  packets total: %d\n", summary.PacketsTotal)
	fmt.Fprintf(out, "  ipv4 packets: %d\n", summary.IPv4Packets)
	fmt.Fprintf(out, "  tcp packets: %d\n", summary.TCPPackets)
	fmt.Fprintf(out, "  alphaess tcp packets: %d\n", summary.MatchedPackets)
	fmt.Fprintf(out, "  alphaess payload packets: %d\n", summary.PayloadPackets)
	fmt.Fprintf(out, "  alphaess payload bytes: %d\n", summary.PayloadBytes)
	fmt.Fprintf(out, "  reassembled stream chunks: %d\n", summary.StreamChunks)
	fmt.Fprintf(out, "  reassembled stream bytes: %d\n", summary.StreamBytes)
	fmt.Fprintf(out, "  alphaess json messages: %d\n", summary.JSONMessages)
	fmt.Fprintf(out, "  alphaess json bytes: %d\n", summary.JSONBytes)
	fmt.Fprintf(out, "  mqtt publish operations: %d\n", summary.MQTTPublishes)
	fmt.Fprintf(out, "  mqtt payload bytes: %d\n", summary.MQTTPayloadBytes)
	fmt.Fprintf(out, "  mqtt status operations: %d\n", summary.StatusPublishes)
	fmt.Fprintf(out, "  mqtt status bytes: %d\n", summary.StatusPayloadBytes)
	fmt.Fprintf(out, "  mqtt discovery operations: %d\n", summary.DiscoveryPublishes)
	fmt.Fprintf(out, "  mqtt discovery bytes: %d\n", summary.DiscoveryPayloadBytes)
	if !summary.FirstPacketTime.IsZero() && !summary.LastPacketTime.IsZero() {
		fmt.Fprintf(out, "  capture duration: %s\n", summary.LastPacketTime.Sub(summary.FirstPacketTime))
	}
}

func isVerbose(cfg config.Config) bool {
	switch strings.ToLower(cfg.Logging.Level) {
	case "debug", "trace", "verbose":
		return true
	default:
		return false
	}
}

var errSetupAborted = errors.New("setup aborted")

func completeConfigInteractive(cfg *config.Config, path string, existed bool, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	changed := false

	if cfg.Input.Mode == "" {
		switch {
		case cfg.Input.File != "":
			cfg.Input.Mode = config.InputModeFile
		case cfg.Input.Interface != "" || cfg.Capture.Interface != "":
			cfg.Input.Mode = config.InputModeInterface
		default:
			if err := promptInputSource(reader, out, cfg); err != nil {
				return err
			}
		}
		changed = true
	}

	switch cfg.Input.Mode {
	case config.InputModeInterface:
		if cfg.Input.Interface == "" && cfg.Capture.Interface != "" {
			cfg.Input.Interface = cfg.Capture.Interface
			changed = true
		}
		if cfg.Input.Interface == "" {
			if err := promptInputSource(reader, out, cfg); err != nil {
				return err
			}
			changed = true
		}
		if cfg.Input.Mode == config.InputModeFile {
			break
		}
		if cfg.Input.Interface == "" {
			value, err := promptRequired(reader, out, "Network interface or PCAP/PCAPNG file")
			if err != nil {
				return err
			}
			applyInputSource(cfg, value)
			changed = true
		}
	case config.InputModeFile:
		if cfg.Input.File == "" {
			value, err := promptRequired(reader, out, "PCAP/PCAPNG file")
			if err != nil {
				return err
			}
			cfg.Input.File = value
			changed = true
		}
	default:
		return fmt.Errorf("unsupported input mode %q", cfg.Input.Mode)
	}

	if cfg.Capture.Interface == "" && cfg.Input.Mode == config.InputModeInterface {
		cfg.Capture.Interface = cfg.Input.Interface
		changed = true
	}
	if cfg.Capture.SourceIP == "" {
		value, err := promptRequired(reader, out, "AlphaESS source IP")
		if err != nil {
			return err
		}
		cfg.Capture.SourceIP = value
		changed = true
	}
	if cfg.Capture.CloudIP == "" {
		value, err := promptRequired(reader, out, "AlphaESS cloud IP")
		if err != nil {
			return err
		}
		cfg.Capture.CloudIP = value
		changed = true
	}
	if cfg.Capture.CloudPort == 0 {
		value, err := promptInt(reader, out, "AlphaESS cloud TCP port")
		if err != nil {
			return err
		}
		cfg.Capture.CloudPort = value
		changed = true
	}
	if cfg.MQTT.Broker == "" {
		value, err := promptRequired(reader, out, "MQTT broker URL")
		if err != nil {
			return err
		}
		cfg.MQTT.Broker = value
		changed = true
	}
	if cfg.MQTT.TopicBase == "" {
		value, err := promptRequired(reader, out, "MQTT topic base")
		if err != nil {
			return err
		}
		cfg.MQTT.TopicBase = value
		changed = true
	}
	if cfg.HomeAssistant.DiscoveryPrefix == "" {
		value, err := promptRequired(reader, out, "Home Assistant discovery prefix")
		if err != nil {
			return err
		}
		cfg.HomeAssistant.DiscoveryPrefix = value
		changed = true
	}
	if cfg.HomeAssistant.DeviceID == "" {
		value, err := promptRequired(reader, out, "Home Assistant device ID")
		if err != nil {
			return err
		}
		cfg.HomeAssistant.DeviceID = value
		changed = true
	}
	if cfg.HomeAssistant.DeviceName == "" {
		value, err := promptRequired(reader, out, "Home Assistant device name")
		if err != nil {
			return err
		}
		cfg.HomeAssistant.DeviceName = value
		changed = true
	}

	if !changed {
		return nil
	}

	action := "save this new config"
	if existed {
		action = "update the existing config"
	}
	answer, err := promptChoice(reader, out, fmt.Sprintf("%s at %s? [ja/nein/abbrechen]", action, path), []string{"ja", "nein", "abbrechen"})
	if err != nil {
		return err
	}
	switch answer {
	case "ja":
		return config.Save(path, *cfg)
	case "nein":
		return nil
	case "abbrechen":
		return errSetupAborted
	default:
		return fmt.Errorf("unexpected answer %q", answer)
	}
}

func promptInputSource(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	value, err := promptRequired(reader, out, "Network interface or PCAP/PCAPNG file")
	if err != nil {
		return err
	}
	applyInputSource(cfg, value)
	return nil
}

func applyInputSource(cfg *config.Config, value string) {
	value = cleanPromptValue(value)
	if config.LooksLikeCaptureFile(value) {
		cfg.Input.Mode = config.InputModeFile
		cfg.Input.File = value
		cfg.Input.Interface = ""
		return
	}

	cfg.Input.Mode = config.InputModeInterface
	cfg.Input.Interface = value
	cfg.Capture.Interface = value
}

func cleanPromptValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func promptRequired(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	for {
		value, err := promptLine(reader, out, label+": ")
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
		fmt.Fprintln(out, "Please enter a value.")
	}
}

func promptInt(reader *bufio.Reader, out io.Writer, label string) (int, error) {
	for {
		value, err := promptRequired(reader, out, label)
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed, nil
		}
		fmt.Fprintln(out, "Please enter a positive number.")
	}
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, choices []string) (string, error) {
	valid := map[string]struct{}{}
	for _, choice := range choices {
		valid[choice] = struct{}{}
	}

	for {
		value, err := promptLine(reader, out, label+": ")
		if err != nil {
			return "", err
		}
		value = strings.ToLower(value)
		if _, ok := valid[value]; ok {
			return value, nil
		}
		fmt.Fprintf(out, "Please choose one of: %s\n", strings.Join(choices, ", "))
	}
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if err != nil && errors.Is(err, io.EOF) && value == "" {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
