package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Heistergand/AlphaESS-to-MQTT_T10-HV/internal/alphaess"
	"github.com/Heistergand/AlphaESS-to-MQTT_T10-HV/internal/homeassistant"
	"github.com/Heistergand/AlphaESS-to-MQTT_T10-HV/internal/mqtt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

type FileOptions struct {
	Path      string
	SourceIP  string
	CloudIP   string
	CloudPort uint16
	Realtime  bool
	Verbose   bool
	Writer    io.Writer

	TopicBase       string
	DeviceID        string
	DeviceName      string
	DiscoveryPrefix string
	Sensors         []homeassistant.Sensor
	Publisher       mqtt.Publisher
}

type FileSummary struct {
	PacketsTotal          int
	IPv4Packets           int
	TCPPackets            int
	MatchedPackets        int
	PayloadPackets        int
	PayloadBytes          int
	StreamChunks          int
	StreamBytes           int
	JSONMessages          int
	JSONBytes             int
	MQTTPublishes         int
	MQTTPayloadBytes      int
	StatusPublishes       int
	StatusPayloadBytes    int
	DiscoveryPublishes    int
	DiscoveryPayloadBytes int
	FirstPacketTime       time.Time
	LastPacketTime        time.Time
}

type packetDataReader interface {
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
	LinkType() layers.LinkType
}

func ReadFile(ctx context.Context, opts FileOptions) (FileSummary, error) {
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	if opts.Path == "" {
		return FileSummary{}, errors.New("pcap file path is required")
	}

	reader, file, format, err := openPacketFile(opts.Path)
	if err != nil {
		return FileSummary{}, err
	}
	defer file.Close()

	if opts.Verbose {
		fmt.Fprintf(opts.Writer, "verbose: reading %s as %s\n", opts.Path, format)
	}

	processor, err := NewProcessor(ctx, ProcessorOptions{
		SourceIP:        opts.SourceIP,
		CloudIP:         opts.CloudIP,
		CloudPort:       opts.CloudPort,
		Verbose:         opts.Verbose,
		Writer:          opts.Writer,
		TopicBase:       opts.TopicBase,
		DeviceID:        opts.DeviceID,
		DeviceName:      opts.DeviceName,
		DiscoveryPrefix: opts.DiscoveryPrefix,
		Sensors:         opts.Sensors,
		Publisher:       opts.Publisher,
	})
	if err != nil {
		return FileSummary{}, err
	}

	sourceIP := net.ParseIP(opts.SourceIP)
	if sourceIP == nil {
		return FileSummary{}, fmt.Errorf("invalid source IP %q", opts.SourceIP)
	}
	cloudIP := net.ParseIP(opts.CloudIP)
	if cloudIP == nil {
		return FileSummary{}, fmt.Errorf("invalid cloud IP %q", opts.CloudIP)
	}

	var previousTimestamp time.Time

	for {
		data, ci, err := reader.ReadPacketData()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return processor.Summary(), err
		}

		if err := waitForReplayTime(ctx, opts.Realtime, previousTimestamp, ci.Timestamp); err != nil {
			return processor.Summary(), err
		}
		previousTimestamp = ci.Timestamp

		processor.AddPacket(ci.Timestamp)

		packet := gopacket.NewPacket(data, reader.LinkType(), gopacket.Lazy)
		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer == nil {
			continue
		}
		processor.AddIPv4Packet()

		tcpLayer := packet.Layer(layers.LayerTypeTCP)
		if tcpLayer == nil {
			continue
		}
		processor.AddTCPPacket()

		ip := ipLayer.(*layers.IPv4)
		tcp := tcpLayer.(*layers.TCP)
		if !matchesAlphaESSFlow(ip, tcp, sourceIP, cloudIP, opts.CloudPort) {
			continue
		}
		if err := processor.ProcessDecodedTCP(ctx, processor.Summary().PacketsTotal, ci.Timestamp, ip, tcp); err != nil {
			return processor.Summary(), err
		}
	}

	return processor.Summary(), nil
}

func openPacketFile(path string) (packetDataReader, *os.File, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, "", err
	}

	ngReader, err := pcapgo.NewNgReader(file, pcapgo.NgReaderOptions{})
	if err == nil {
		return ngReader, file, "pcapng", nil
	}

	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		file.Close()
		return nil, nil, "", seekErr
	}

	pcapReader, pcapErr := pcapgo.NewReader(file)
	if pcapErr == nil {
		return pcapReader, file, "pcap", nil
	}

	file.Close()
	return nil, nil, "", fmt.Errorf("open pcapng: %v; open pcap: %v", err, pcapErr)
}

func matchesAlphaESSFlow(ip *layers.IPv4, tcp *layers.TCP, sourceIP, cloudIP net.IP, cloudPort uint16) bool {
	fromAlphaESS := isAlphaESSOutbound(ip, tcp, sourceIP, cloudIP, cloudPort)
	toAlphaESS := ip.SrcIP.Equal(cloudIP) && ip.DstIP.Equal(sourceIP) && uint16(tcp.SrcPort) == cloudPort
	return fromAlphaESS || toAlphaESS
}

func isAlphaESSOutbound(ip *layers.IPv4, tcp *layers.TCP, sourceIP, cloudIP net.IP, cloudPort uint16) bool {
	return ip.SrcIP.Equal(sourceIP) && ip.DstIP.Equal(cloudIP) && uint16(tcp.DstPort) == cloudPort
}

func tcpFlowID(ip *layers.IPv4, tcp *layers.TCP) string {
	return fmt.Sprintf("%s:%d->%s:%d", ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort)
}

func decoderForFlow(decoders map[string]*alphaess.Decoder, flowID string) *alphaess.Decoder {
	decoder, ok := decoders[flowID]
	if ok {
		return decoder
	}
	decoder = alphaess.NewDecoder()
	decoders[flowID] = decoder
	return decoder
}

func firstFieldNames(fields map[string]any, limit int) []string {
	names := make([]string, 0, min(len(fields), limit))
	for name := range fields {
		names = append(names, name)
		if len(names) == limit {
			break
		}
	}
	return names
}

func waitForReplayTime(ctx context.Context, realtime bool, previous, current time.Time) error {
	if !realtime || previous.IsZero() || current.IsZero() {
		return nil
	}

	delay := current.Sub(previous)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
