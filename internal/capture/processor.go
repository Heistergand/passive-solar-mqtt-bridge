package capture

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/alphaess"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/homeassistant"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/mqtt"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/reassembly"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/state"
	"github.com/google/gopacket/layers"
)

type ProcessorOptions struct {
	SourceIP        string
	CloudIP         string
	CloudPort       uint16
	Verbose         bool
	Writer          io.Writer
	TopicBase       string
	DeviceID        string
	DeviceName      string
	DiscoveryPrefix string
	Sensors         []homeassistant.Sensor
	Publisher       mqtt.Publisher
}

type Processor struct {
	opts      ProcessorOptions
	sourceIP  net.IP
	cloudIP   net.IP
	assembler *reassembly.Assembler
	decoders  map[string]*alphaess.Decoder
	fields    map[string]any
	summary   FileSummary
}

func NewProcessor(ctx context.Context, opts ProcessorOptions) (*Processor, error) {
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}

	sourceIP := net.ParseIP(opts.SourceIP)
	if sourceIP == nil {
		return nil, fmt.Errorf("invalid source IP %q", opts.SourceIP)
	}
	cloudIP := net.ParseIP(opts.CloudIP)
	if cloudIP == nil {
		return nil, fmt.Errorf("invalid cloud IP %q", opts.CloudIP)
	}

	processor := &Processor{
		opts:      opts,
		sourceIP:  sourceIP,
		cloudIP:   cloudIP,
		assembler: reassembly.NewAssembler(),
		decoders:  map[string]*alphaess.Decoder{},
		fields:    map[string]any{},
	}

	if err := processor.planAvailability(ctx); err != nil {
		return nil, err
	}
	if err := processor.planDiscovery(ctx); err != nil {
		return nil, err
	}

	return processor, nil
}

func (p *Processor) Summary() FileSummary {
	return p.summary
}

func (p *Processor) ProcessDecodedTCP(ctx context.Context, packetNumber int, timestamp time.Time, ip *layers.IPv4, tcp *layers.TCP) error {
	if !matchesAlphaESSFlow(ip, tcp, p.sourceIP, p.cloudIP, p.opts.CloudPort) {
		return nil
	}

	p.summary.MatchedPackets++
	payloadLen := len(tcp.Payload)
	if payloadLen > 0 {
		p.summary.PayloadPackets++
		p.summary.PayloadBytes += payloadLen
	}

	if payloadLen > 0 && isAlphaESSOutbound(ip, tcp, p.sourceIP, p.cloudIP, p.opts.CloudPort) {
		if err := p.processOutboundPayload(ctx, ip, tcp); err != nil {
			return err
		}
	}

	if p.opts.Verbose {
		fmt.Fprintf(
			p.opts.Writer,
			"verbose: packet=%d time=%s %s:%d -> %s:%d tcp_payload=%d\n",
			packetNumber,
			timestamp.Format(time.RFC3339Nano),
			ip.SrcIP,
			tcp.SrcPort,
			ip.DstIP,
			tcp.DstPort,
			payloadLen,
		)
	}

	return nil
}

func (p *Processor) AddPacket(timestamp time.Time) {
	p.summary.PacketsTotal++
	if p.summary.FirstPacketTime.IsZero() {
		p.summary.FirstPacketTime = timestamp
	}
	p.summary.LastPacketTime = timestamp
}

func (p *Processor) AddIPv4Packet() {
	p.summary.IPv4Packets++
}

func (p *Processor) AddTCPPacket() {
	p.summary.TCPPackets++
}

func (p *Processor) planAvailability(ctx context.Context) error {
	message := mqtt.StatusMessage(p.opts.TopicBase, "online")
	p.summary.StatusPublishes++
	p.summary.StatusPayloadBytes += len(message.Payload)
	if p.opts.Publisher != nil {
		if err := p.opts.Publisher.PublishJSON(ctx, message.Topic, message.Payload, message.Retained); err != nil {
			return err
		}
	}
	if p.opts.Verbose {
		fmt.Fprintf(
			p.opts.Writer,
			"verbose: mqtt status %s topic=%s payload=%s retained=%t\n",
			p.publishVerb(),
			message.Topic,
			message.Payload,
			message.Retained,
		)
	}
	return nil
}

func (p *Processor) planDiscovery(ctx context.Context) error {
	sensors := p.opts.Sensors
	if len(sensors) == 0 {
		sensors = homeassistant.BaseSensors()
	}
	discoveryMessages, err := homeassistant.DiscoveryMessages(
		p.opts.DiscoveryPrefix,
		p.opts.TopicBase,
		homeassistant.Device{
			ID:   p.opts.DeviceID,
			Name: p.opts.DeviceName,
		},
		sensors,
	)
	if err != nil {
		return err
	}

	for _, message := range discoveryMessages {
		p.summary.DiscoveryPublishes++
		p.summary.DiscoveryPayloadBytes += len(message.Payload)
		if p.opts.Publisher != nil {
			if err := p.opts.Publisher.PublishJSON(ctx, message.Topic, message.Payload, message.Retained); err != nil {
				return err
			}
		}
		if p.opts.Verbose {
			fmt.Fprintf(
				p.opts.Writer,
				"verbose: mqtt discovery %s topic=%s payload_bytes=%d retained=%t\n",
				p.publishVerb(),
				message.Topic,
				len(message.Payload),
				message.Retained,
			)
		}
	}

	return nil
}

func (p *Processor) processOutboundPayload(ctx context.Context, ip *layers.IPv4, tcp *layers.TCP) error {
	flowID := tcpFlowID(ip, tcp)
	chunks := p.assembler.Push(reassembly.Segment{
		FlowID: flowID,
		Seq:    tcp.Seq,
		Data:   tcp.Payload,
	})
	for _, chunk := range chunks {
		p.summary.StreamChunks++
		p.summary.StreamBytes += len(chunk.Data)
		decoder := decoderForFlow(p.decoders, chunk.FlowID)
		messages, err := decoder.Push(chunk.Data)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if err := p.processMessage(ctx, chunk.FlowID, message); err != nil {
				return err
			}
		}
		if p.opts.Verbose {
			fmt.Fprintf(
				p.opts.Writer,
				"verbose: stream flow=%s seq=%d chunk_bytes=%d stream_bytes=%d\n",
				chunk.FlowID,
				tcp.Seq,
				len(chunk.Data),
				p.summary.StreamBytes,
			)
		}
	}
	return nil
}

func (p *Processor) processMessage(ctx context.Context, flowID string, message alphaess.Message) error {
	p.summary.JSONMessages++
	p.summary.JSONBytes += len(message.RawJSON)
	p.mergeFields(message.Fields)
	payload, err := state.Marshal(p.fields, p.opts.DeviceID, p.opts.DeviceName)
	if err != nil {
		return err
	}
	publish := mqtt.RawStateMessage(p.opts.TopicBase, payload)
	p.summary.MQTTPublishes++
	p.summary.MQTTPayloadBytes += len(publish.Payload)
	if p.opts.Publisher != nil {
		if err := p.opts.Publisher.PublishJSON(ctx, publish.Topic, publish.Payload, publish.Retained); err != nil {
			return err
		}
	}
	if p.opts.Verbose {
		fmt.Fprintf(
			p.opts.Writer,
			"verbose: alphaess json flow=%s bytes=%d fields=%v\n",
			flowID,
			len(message.RawJSON),
			firstFieldNames(message.Fields, 8),
		)
		fmt.Fprintf(
			p.opts.Writer,
			"verbose: mqtt %s topic=%s payload_bytes=%d retained=%t\n",
			p.publishVerb(),
			publish.Topic,
			len(publish.Payload),
			publish.Retained,
		)
	}
	return nil
}

func (p *Processor) mergeFields(fields map[string]any) {
	for key, value := range fields {
		p.fields[key] = value
	}
}

func (p *Processor) publishVerb() string {
	if p.opts.Publisher != nil {
		return "published"
	}
	return "planned"
}
