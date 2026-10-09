package mqtt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Broker       string
	ClientID     string
	Username     string
	PasswordFile string
	KeepAlive    time.Duration
	WillTopic    string
	WillPayload  []byte
	WillRetained bool
	OnlineTopic  string
}

type Client struct {
	mu   sync.Mutex
	opts Options
	conn net.Conn
	stop chan struct{}
}

func NewClient(opts Options) *Client {
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = 30 * time.Second
	}
	if opts.KeepAlive < time.Second {
		opts.KeepAlive = time.Second
	}
	return &Client{opts: opts}
}

func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return errors.New("mqtt client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked(ctx)
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}

	network, address, err := brokerAddress(c.opts.Broker)
	if err != nil {
		return err
	}

	var dialer = net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	c.conn = conn
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	password, err := readPassword(c.opts.PasswordFile)
	if err != nil {
		c.closeConnectionOnly()
		return err
	}
	if password != "" && c.opts.Username == "" {
		c.closeConnectionOnly()
		return errors.New("mqtt username is required when password_file is configured")
	}

	packet := connectPacket(c.opts.ClientID, c.opts.Username, password, uint16(c.opts.KeepAlive/time.Second), willOptions{
		Topic:    c.opts.WillTopic,
		Payload:  c.opts.WillPayload,
		Retained: c.opts.WillRetained,
	})
	if err := writeFull(c.conn, packet); err != nil {
		c.closeConnectionOnly()
		return err
	}

	response := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, response); err != nil {
		c.closeConnectionOnly()
		return err
	}
	if response[0] != 0x20 || response[1] != 0x02 || response[2] != 0x00 || response[3] != 0x00 {
		c.closeConnectionOnly()
		return fmt.Errorf("mqtt connect rejected: % x", response)
	}

	conn.SetDeadline(time.Time{})
	if c.opts.OnlineTopic != "" {
		if err := c.writePacket(ctx, publishPacket(c.opts.OnlineTopic, []byte("online"), true)); err != nil {
			c.closeConnectionOnly()
			return err
		}
	}
	if c.opts.KeepAlive > 0 {
		c.stop = make(chan struct{})
		go c.keepAlive(conn, c.stop)
	}
	return nil
}

func (c *Client) PublishJSON(ctx context.Context, topic string, payload []byte, retained bool) error {
	if c == nil {
		return errors.New("mqtt client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.connectLocked(ctx); err != nil {
			return err
		}
	}

	packet := publishPacket(topic, payload, retained)
	if err := c.writePacket(ctx, packet); err == nil {
		return nil
	}

	c.closeConnectionOnly()
	if err := c.connectLocked(ctx); err != nil {
		return err
	}
	return c.writePacket(ctx, packet)
}

func (c *Client) writePacket(ctx context.Context, packet []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.conn.SetWriteDeadline(deadline)
	defer c.conn.SetWriteDeadline(time.Time{})
	return writeFull(c.conn, packet)
}

// Serialize PINGREQ/PINGRESP with publishes; this QoS 0 client expects no
// other server packets after CONNACK. A failed ping invalidates the socket,
// so the next publication reconnects and restores retained availability.
func (c *Client) keepAlive(conn net.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(c.opts.KeepAlive / 2)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.conn != conn {
				c.mu.Unlock()
				return
			}
			conn.SetDeadline(time.Now().Add(min(c.opts.KeepAlive/2, 5*time.Second)))
			err := writeFull(conn, []byte{0xc0, 0x00})
			var response [2]byte
			if err == nil {
				_, err = io.ReadFull(conn, response[:])
			}
			conn.SetDeadline(time.Time{})
			if err != nil || response != [2]byte{0xd0, 0x00} {
				c.closeConnectionOnly()
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
		}
	}
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}

	_ = c.writePacket(context.Background(), []byte{0xe0, 0x00})
	c.closeConnectionOnly()
	return nil
}

func (c *Client) closeConnectionOnly() {
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	if c.conn == nil {
		return
	}
	_ = c.conn.Close()
	c.conn = nil
}

func PublishAll(ctx context.Context, publisher Publisher, messages []Message) error {
	if publisher == nil {
		return nil
	}
	if err := publisher.Connect(ctx); err != nil {
		return err
	}
	defer publisher.Close()

	for _, message := range messages {
		if err := publisher.PublishJSON(ctx, message.Topic, message.Payload, message.Retained); err != nil {
			return err
		}
	}
	return nil
}

func brokerAddress(raw string) (network, address string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme != "tcp" {
		return "", "", fmt.Errorf("unsupported mqtt broker scheme %q", parsed.Scheme)
	}
	host := parsed.Host
	if host == "" {
		return "", "", fmt.Errorf("mqtt broker host is required")
	}
	if !strings.Contains(host, ":") {
		host += ":1883"
	}
	return "tcp", host, nil
}

func readPassword(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

type willOptions struct {
	Topic    string
	Payload  []byte
	Retained bool
}

func connectPacket(clientID, username, password string, keepAliveSeconds uint16, will willOptions) []byte {
	var variableAndPayload []byte
	variableAndPayload = appendUTF8(variableAndPayload, "MQTT")
	variableAndPayload = append(variableAndPayload, 0x04)

	flags := byte(0x02) // clean session
	hasWill := will.Topic != ""
	if hasWill {
		flags |= 0x04
		if will.Retained {
			flags |= 0x20
		}
	}
	if username != "" {
		flags |= 0x80
	}
	if password != "" {
		flags |= 0x40
	}
	variableAndPayload = append(variableAndPayload, flags)
	variableAndPayload = append(variableAndPayload, byte(keepAliveSeconds>>8), byte(keepAliveSeconds))
	variableAndPayload = appendUTF8(variableAndPayload, clientID)
	if hasWill {
		variableAndPayload = appendUTF8(variableAndPayload, will.Topic)
		variableAndPayload = appendUTF8(variableAndPayload, string(will.Payload))
	}
	if username != "" {
		variableAndPayload = appendUTF8(variableAndPayload, username)
	}
	if password != "" {
		variableAndPayload = appendUTF8(variableAndPayload, password)
	}

	packet := []byte{0x10}
	packet = append(packet, encodeRemainingLength(len(variableAndPayload))...)
	packet = append(packet, variableAndPayload...)
	return packet
}

func publishPacket(topic string, payload []byte, retained bool) []byte {
	header := byte(0x30)
	if retained {
		header |= 0x01
	}

	var variableAndPayload []byte
	variableAndPayload = appendUTF8(variableAndPayload, topic)
	variableAndPayload = append(variableAndPayload, payload...)

	packet := []byte{header}
	packet = append(packet, encodeRemainingLength(len(variableAndPayload))...)
	packet = append(packet, variableAndPayload...)
	return packet
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[n:]
	}
	return nil
}

func appendUTF8(dst []byte, value string) []byte {
	dst = append(dst, byte(len(value)>>8), byte(len(value)))
	dst = append(dst, value...)
	return dst
}

func encodeRemainingLength(length int) []byte {
	var encoded []byte
	for {
		digit := byte(length % 128)
		length /= 128
		if length > 0 {
			digit |= 0x80
		}
		encoded = append(encoded, digit)
		if length == 0 {
			break
		}
	}
	return encoded
}
