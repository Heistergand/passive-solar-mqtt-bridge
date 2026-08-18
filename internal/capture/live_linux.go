//go:build linux

package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/homeassistant"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/mqtt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type LiveOptions struct {
	Interface string
	SourceIP  string
	CloudIP   string
	CloudPort uint16
	Verbose   bool
	Writer    io.Writer

	TopicBase       string
	DeviceID        string
	DeviceName      string
	DiscoveryPrefix string
	Sensors         []homeassistant.Sensor
	Publisher       mqtt.Publisher
}

const autoDetectInterface = "auto"

func ReadLive(ctx context.Context, opts LiveOptions) (FileSummary, error) {
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	if opts.Interface == "" {
		return FileSummary{}, errors.New("capture interface is required")
	}

	if strings.EqualFold(opts.Interface, autoDetectInterface) {
		ifaceName, err := detectLiveInterface(ctx, detectOptions{
			SourceIP:  opts.SourceIP,
			CloudIP:   opts.CloudIP,
			CloudPort: opts.CloudPort,
			Verbose:   opts.Verbose,
			Writer:    opts.Writer,
			Timeout:   2 * time.Minute,
		})
		if err != nil {
			return FileSummary{}, err
		}
		opts.Interface = ifaceName
	}

	iface, err := net.InterfaceByName(opts.Interface)
	if err != nil {
		fmt.Fprintf(opts.Writer, "warning: capture interface %q is not available; trying auto-detection\n", opts.Interface)
		ifaceName, detectErr := detectLiveInterface(ctx, detectOptions{
			SourceIP:  opts.SourceIP,
			CloudIP:   opts.CloudIP,
			CloudPort: opts.CloudPort,
			Verbose:   opts.Verbose,
			Writer:    opts.Writer,
			Timeout:   2 * time.Minute,
		})
		if detectErr != nil {
			return FileSummary{}, fmt.Errorf("capture interface %q is not available and auto-detection failed: %w", opts.Interface, detectErr)
		}
		opts.Interface = ifaceName
		iface, err = net.InterfaceByName(opts.Interface)
		if err != nil {
			return FileSummary{}, err
		}
	}

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ALL)))
	if err != nil {
		return FileSummary{}, err
	}
	defer syscall.Close(fd)

	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ALL),
		Ifindex:  iface.Index,
	}); err != nil {
		return FileSummary{}, err
	}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 1})

	if opts.Verbose {
		fmt.Fprintf(opts.Writer, "verbose: live capture started on %s\n", opts.Interface)
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

	nextHeartbeat := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return processor.Summary(), nil
		default:
		}

		if opts.Verbose && !time.Now().Before(nextHeartbeat) {
			printLiveHeartbeat(opts.Writer, opts.Interface, processor.Summary())
			nextHeartbeat = time.Now().Add(30 * time.Second)
		}

		buffer := make([]byte, 65536)
		n, _, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				select {
				case <-ctx.Done():
					return processor.Summary(), nil
				default:
					continue
				}
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return processor.Summary(), err
		}
		data := buffer[:n]
		timestamp := time.Now()

		processor.AddPacket(timestamp)

		packet := gopacket.NewPacket(data, layers.LinkTypeEthernet, gopacket.Lazy)
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
		if err := processor.ProcessDecodedTCP(ctx, processor.Summary().PacketsTotal, timestamp, ip, tcp); err != nil {
			return processor.Summary(), err
		}
	}
}

func printLiveHeartbeat(out io.Writer, iface string, summary FileSummary) {
	fmt.Fprintf(
		out,
		"verbose: live capture still running interface=%s packets=%d alphaess_tcp=%d json=%d mqtt_state=%d\n",
		iface,
		summary.PacketsTotal,
		summary.MatchedPackets,
		summary.JSONMessages,
		summary.MQTTPublishes,
	)
}

type detectOptions struct {
	SourceIP  string
	CloudIP   string
	CloudPort uint16
	Verbose   bool
	Writer    io.Writer
	Timeout   time.Duration
}

func detectLiveInterface(ctx context.Context, opts detectOptions) (string, error) {
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}

	sourceIP := net.ParseIP(opts.SourceIP)
	if sourceIP == nil {
		return "", fmt.Errorf("invalid AlphaESS source IP %q", opts.SourceIP)
	}
	cloudIP := net.ParseIP(opts.CloudIP)
	if cloudIP == nil {
		return "", fmt.Errorf("invalid AlphaESS cloud IP %q", opts.CloudIP)
	}

	interfaces, err := candidateInterfaceNames()
	if err != nil {
		return "", err
	}
	if len(interfaces) == 0 {
		return "", errors.New("no active non-loopback capture interfaces found")
	}

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ALL)))
	if err != nil {
		return "", err
	}
	defer syscall.Close(fd)

	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 1})

	if opts.Verbose {
		fmt.Fprintf(opts.Writer, "verbose: detecting live interface for AlphaESS flow %s -> %s:%d\n", opts.SourceIP, opts.CloudIP, opts.CloudPort)
	}

	deadline := time.Now().Add(opts.Timeout)
	nextHeartbeat := time.Now().Add(10 * time.Second)
	buffer := make([]byte, 65536)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if opts.Verbose && !time.Now().Before(nextHeartbeat) {
			fmt.Fprintf(opts.Writer, "verbose: waiting for AlphaESS traffic to identify capture interface\n")
			nextHeartbeat = time.Now().Add(10 * time.Second)
		}

		n, from, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return "", err
		}

		link, ok := from.(*syscall.SockaddrLinklayer)
		if !ok {
			continue
		}
		ifaceName, ok := interfaces[link.Ifindex]
		if !ok {
			continue
		}
		if packetMatchesAlphaESSFlow(buffer[:n], sourceIP, cloudIP, opts.CloudPort) {
			fmt.Fprintf(opts.Writer, "auto-detected capture interface: %s\n", ifaceName)
			return ifaceName, nil
		}
	}

	return "", fmt.Errorf("no AlphaESS TCP traffic seen within %s on active interfaces", opts.Timeout)
}

func candidateInterfaceNames() (map[int]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	names := map[int]string{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		names[iface.Index] = iface.Name
	}
	return names, nil
}

func packetMatchesAlphaESSFlow(data []byte, sourceIP, cloudIP net.IP, cloudPort uint16) bool {
	packet := gopacket.NewPacket(data, layers.LinkTypeEthernet, gopacket.Lazy)
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return false
	}
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		return false
	}
	return matchesAlphaESSFlow(ipLayer.(*layers.IPv4), tcpLayer.(*layers.TCP), sourceIP, cloudIP, cloudPort)
}

func htons(value int) uint16 {
	return uint16((value&0xff)<<8 | (value>>8)&0xff)
}
