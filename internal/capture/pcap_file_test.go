package capture

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/homeassistant"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/mqtt"
	"github.com/google/gopacket/layers"
)

func TestMatchesAlphaESSFlowOutbound(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("198.51.100.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
	}

	if !matchesAlphaESSFlow(ip, tcp, net.ParseIP("192.0.2.10"), net.ParseIP("198.51.100.10"), 7777) {
		t.Fatal("matchesAlphaESSFlow() = false, want true")
	}
}

func TestMatchesAlphaESSFlowInbound(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP: net.ParseIP("198.51.100.10"),
		DstIP: net.ParseIP("192.0.2.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 7777,
		DstPort: 51170,
	}

	if !matchesAlphaESSFlow(ip, tcp, net.ParseIP("192.0.2.10"), net.ParseIP("198.51.100.10"), 7777) {
		t.Fatal("matchesAlphaESSFlow() = false, want true")
	}
}

func TestMatchesAlphaESSFlowRejectsOtherTraffic(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("1.1.1.1"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
	}

	if matchesAlphaESSFlow(ip, tcp, net.ParseIP("192.0.2.10"), net.ParseIP("198.51.100.10"), 7777) {
		t.Fatal("matchesAlphaESSFlow() = true, want false")
	}
}

func TestIsAlphaESSOutbound(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("198.51.100.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
	}

	if !isAlphaESSOutbound(ip, tcp, net.ParseIP("192.0.2.10"), net.ParseIP("198.51.100.10"), 7777) {
		t.Fatal("isAlphaESSOutbound() = false, want true")
	}
}

func TestTCPFlowID(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("198.51.100.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
	}

	if got := tcpFlowID(ip, tcp); got != "192.0.2.10:51170->198.51.100.10:7777" {
		t.Fatalf("tcpFlowID() = %q", got)
	}
}

func TestWaitForReplayTimeHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForReplayTime(ctx, true, time.Unix(1, 0), time.Unix(2, 0))
	if err == nil {
		t.Fatal("waitForReplayTime() error = nil, want context error")
	}
}

func TestNewProcessorPublishesAvailabilityBeforeDiscovery(t *testing.T) {
	publisher := &recordingPublisher{}
	processor, err := NewProcessor(context.Background(), ProcessorOptions{
		SourceIP:        "192.0.2.10",
		CloudIP:         "198.51.100.10",
		CloudPort:       7777,
		TopicBase:       "alphaess",
		DeviceID:        "alphaess_t10_hv",
		DeviceName:      "AlphaESS SMILE-T10-HV",
		DiscoveryPrefix: "homeassistant",
		Publisher:       publisher,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}

	summary := processor.Summary()
	if summary.StatusPublishes != 1 {
		t.Fatalf("StatusPublishes = %d, want 1", summary.StatusPublishes)
	}
	if summary.DiscoveryPublishes == 0 {
		t.Fatal("DiscoveryPublishes = 0, want discovery messages")
	}
	if len(publisher.messages) == 0 {
		t.Fatal("no messages published")
	}
	first := publisher.messages[0]
	if first.Topic != "alphaess/status" {
		t.Fatalf("first topic = %q, want alphaess/status", first.Topic)
	}
	if string(first.Payload) != "online" {
		t.Fatalf("first payload = %q, want online", first.Payload)
	}
	if !first.Retained {
		t.Fatal("first retained = false, want true")
	}
}

func TestProcessorPublishesMergedLatestState(t *testing.T) {
	publisher := &recordingPublisher{}
	processor, err := NewProcessor(context.Background(), ProcessorOptions{
		SourceIP:        "192.0.2.10",
		CloudIP:         "198.51.100.10",
		CloudPort:       7777,
		TopicBase:       "alphaess",
		DeviceID:        "alphaess_t10_hv",
		DeviceName:      "AlphaESS SMILE-T10-HV",
		DiscoveryPrefix: "homeassistant",
		Publisher:       publisher,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}

	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("198.51.100.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
		Seq:     1000,
	}
	firstPayload := []byte(`xx{"SN":"ALB000000000000","SOC":"33.2","Ppv1":"10"}yy`)
	tcp.Payload = firstPayload
	if err := processor.ProcessDecodedTCP(context.Background(), 1, time.Unix(1, 0), ip, tcp); err != nil {
		t.Fatalf("ProcessDecodedTCP() first error = %v", err)
	}

	tcp.Seq += uint32(len(firstPayload))
	tcp.Payload = []byte(`xx{"EpvTotal":"4988.7","Einput":"3522.96"}yy`)
	if err := processor.ProcessDecodedTCP(context.Background(), 2, time.Unix(2, 0), ip, tcp); err != nil {
		t.Fatalf("ProcessDecodedTCP() second error = %v", err)
	}

	last := publisher.messages[len(publisher.messages)-1]
	if last.Topic != "alphaess/raw/state" {
		t.Fatalf("last topic = %q, want alphaess/raw/state", last.Topic)
	}
	var statePayload struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.Unmarshal(last.Payload, &statePayload); err != nil {
		t.Fatalf("state payload is not JSON: %v", err)
	}
	if statePayload.Values["SOC"] != 33.2 {
		t.Fatalf("SOC = %v, want retained latest value 33.2", statePayload.Values["SOC"])
	}
	if statePayload.Values["EpvTotal"] != 4988.7 {
		t.Fatalf("EpvTotal = %v, want 4988.7", statePayload.Values["EpvTotal"])
	}
}

func TestProcessorPublishesMQTTSequenceToBroker(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	packets := make(chan [][]byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}
		reader := bufio.NewReader(conn)
		connect, err := readMQTTPacket(reader)
		if err != nil {
			return
		}
		if !bytes.Contains(connect, []byte("alphaess/status")) || !bytes.Contains(connect, []byte("offline")) {
			return
		}
		if _, err := conn.Write([]byte{0x20, 0x02, 0x00, 0x00}); err != nil {
			return
		}

		var published [][]byte
		for {
			packet, err := readMQTTPacket(reader)
			if err != nil {
				break
			}
			published = append(published, packet)
			if packet[0] == 0xe0 {
				break
			}
		}
		packets <- published
	}()

	client := mqtt.NewClient(mqtt.Options{
		Broker:       "tcp://" + listener.Addr().String(),
		ClientID:     "alphaess_t10_hv",
		WillTopic:    "alphaess/status",
		WillPayload:  []byte("offline"),
		WillRetained: true,
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	processor, err := NewProcessor(context.Background(), ProcessorOptions{
		SourceIP:        "192.0.2.10",
		CloudIP:         "198.51.100.10",
		CloudPort:       7777,
		TopicBase:       "alphaess",
		DeviceID:        "alphaess_t10_hv",
		DeviceName:      "AlphaESS SMILE-T10-HV",
		DiscoveryPrefix: "homeassistant",
		Publisher:       client,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}

	ip := &layers.IPv4{
		SrcIP: net.ParseIP("192.0.2.10"),
		DstIP: net.ParseIP("198.51.100.10"),
	}
	tcp := &layers.TCP{
		SrcPort: 51170,
		DstPort: 7777,
		Seq:     1000,
	}
	tcp.Payload = []byte(`xx{"SN":"ALB000000000000","SOC":"33.2","Ppv1":"10","Ppv2":"20","Pbat":"600"}yy`)
	if err := processor.ProcessDecodedTCP(context.Background(), 1, time.Unix(1, 0), ip, tcp); err != nil {
		t.Fatalf("ProcessDecodedTCP() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	published := <-packets
	messages := decodePublishMessages(t, published)
	wantMessages := 1 + len(homeassistant.BaseSensors()) + 1
	if len(messages) != wantMessages {
		t.Fatalf("publish messages = %d, want %d", len(messages), wantMessages)
	}
	if messages[0].topic != "alphaess/status" {
		t.Fatalf("first topic = %q, want alphaess/status", messages[0].topic)
	}
	if string(messages[0].payload) != "online" {
		t.Fatalf("first payload = %q, want online", messages[0].payload)
	}
	if !messages[0].retained {
		t.Fatal("first retained = false, want true")
	}
	if messages[1].topic != "homeassistant/sensor/alphaess_t10_hv/soc/config" {
		t.Fatalf("second topic = %q", messages[1].topic)
	}
	last := messages[len(messages)-1]
	if last.topic != "alphaess/raw/state" {
		t.Fatalf("last topic = %q, want alphaess/raw/state", last.topic)
	}
	if !last.retained {
		t.Fatal("last retained = false, want true")
	}

	var statePayload struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.Unmarshal(last.payload, &statePayload); err != nil {
		t.Fatalf("state payload is not JSON: %v", err)
	}
	if statePayload.Values["SOC"] != 33.2 {
		t.Fatalf("SOC = %v, want 33.2", statePayload.Values["SOC"])
	}
	if statePayload.Values["PpvTotal"] != 30 {
		t.Fatalf("PpvTotal = %v, want 30", statePayload.Values["PpvTotal"])
	}
}

type recordingPublisher struct {
	messages []mqtt.Message
}

func (p *recordingPublisher) Connect(context.Context) error {
	return nil
}

func (p *recordingPublisher) PublishJSON(_ context.Context, topic string, payload []byte, retained bool) error {
	p.messages = append(p.messages, mqtt.Message{
		Topic:    topic,
		Payload:  payload,
		Retained: retained,
	})
	return nil
}

func (p *recordingPublisher) Close() error {
	return nil
}

type decodedPublishMessage struct {
	topic    string
	payload  []byte
	retained bool
}

func readMQTTPacket(reader *bufio.Reader) ([]byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	packet := []byte{first}
	remainingLength := 0
	multiplier := 1
	for {
		digit, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		packet = append(packet, digit)
		remainingLength += int(digit&127) * multiplier
		if digit&128 == 0 {
			break
		}
		multiplier *= 128
	}
	body := make([]byte, remainingLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	packet = append(packet, body...)
	return packet, nil
}

func decodePublishMessages(t *testing.T, packets [][]byte) []decodedPublishMessage {
	t.Helper()

	var messages []decodedPublishMessage
	for _, packet := range packets {
		if packet[0]&0xf0 != 0x30 {
			continue
		}
		bodyOffset := mqttBodyOffset(packet)
		if len(packet) < bodyOffset+2 {
			t.Fatalf("short publish packet: % x", packet)
		}
		topicLength := int(packet[bodyOffset])<<8 | int(packet[bodyOffset+1])
		topicStart := bodyOffset + 2
		topicEnd := topicStart + topicLength
		if len(packet) < topicEnd {
			t.Fatalf("short publish topic: % x", packet)
		}
		messages = append(messages, decodedPublishMessage{
			topic:    string(packet[topicStart:topicEnd]),
			payload:  packet[topicEnd:],
			retained: packet[0]&0x01 != 0,
		})
	}
	return messages
}

func mqttBodyOffset(packet []byte) int {
	offset := 1
	for {
		digit := packet[offset]
		offset++
		if digit&128 == 0 {
			return offset
		}
	}
}
