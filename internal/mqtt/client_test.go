package mqtt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrokerAddressAddsDefaultPort(t *testing.T) {
	network, address, err := brokerAddress("tcp://127.0.0.1")
	if err != nil {
		t.Fatalf("brokerAddress() error = %v", err)
	}
	if network != "tcp" {
		t.Fatalf("network = %q", network)
	}
	if address != "127.0.0.1:1883" {
		t.Fatalf("address = %q", address)
	}
}

// Read complete MQTT packets rather than assuming one TCP read per packet.
func readTestPacket(conn net.Conn) ([]byte, error) {
	var header [1]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	packet := []byte{header[0]}
	length, multiplier := 0, 1
	for i := 0; i < 4; i++ {
		var b [1]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return nil, err
		}
		packet = append(packet, b[0])
		length += int(b[0]&127) * multiplier
		if b[0]&128 == 0 {
			body := make([]byte, length)
			_, err := io.ReadFull(conn, body)
			return append(packet, body...), err
		}
		multiplier *= 128
	}
	return nil, errors.New("invalid remaining length")
}

func TestKeepAliveAndAvailabilityAfterReconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		result <- func() error {
			for session := 0; session < 2; session++ {
				conn, err := listener.Accept()
				if err != nil {
					return err
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := readTestPacket(conn); err != nil {
					return err
				}
				if _, err := conn.Write([]byte{0x20, 2, 0, 0}); err != nil {
					return err
				}
				packet, err := readTestPacket(conn)
				if err != nil {
					return err
				}
				if !bytes.Equal(packet, publishPacket("alphaess/status", []byte("online"), true)) {
					return errors.New("connection did not restore retained online first")
				}
				if session == 0 {
					// A healthy idle connection receives pings and remains usable.
					for i := 0; i < 2; i++ {
						packet, err = readTestPacket(conn)
						if err != nil {
							return err
						}
						if !bytes.Equal(packet, []byte{0xc0, 0}) {
							return errors.New("expected ping")
						}
						if _, err := conn.Write([]byte{0xd0, 0}); err != nil {
							return err
						}
					}
					// Then stop answering: the client must detect a missing PINGRESP.
					if _, err := readTestPacket(conn); err != nil {
						return err
					}
					var b [1]byte
					if _, err := conn.Read(b[:]); err != io.EOF {
						return errors.New("client did not close unresponsive connection")
					}
				} else {
					packet, err = readTestPacket(conn)
					if err != nil {
						return err
					}
					if !bytes.Equal(packet, publishPacket("alphaess/raw/state", []byte(`{"SOC":42}`), true)) {
						return errors.New("missing state after reconnect")
					}
				}
			}
			return nil
		}()
	}()
	client := NewClient(Options{Broker: "tcp://" + listener.Addr().String(), ClientID: "test", KeepAlive: time.Second, OnlineTopic: "alphaess/status"})
	defer client.Close()
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		client.mu.Lock()
		disconnected := client.conn == nil
		client.mu.Unlock()
		if disconnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ping timeout not detected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := client.PublishJSON(context.Background(), "alphaess/raw/state", []byte(`{"SOC":42}`), true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("broker timed out")
	}
}

func TestEncodeRemainingLength(t *testing.T) {
	if got := encodeRemainingLength(321); !bytes.Equal(got, []byte{0xc1, 0x02}) {
		t.Fatalf("encoded = % x", got)
	}
}

func TestPublishPacketRetained(t *testing.T) {
	packet := publishPacket("alphaess/raw/state", []byte(`{"ok":true}`), true)

	if packet[0] != 0x31 {
		t.Fatalf("fixed header = %#x", packet[0])
	}
	if !bytes.Contains(packet, []byte("alphaess/raw/state")) {
		t.Fatalf("packet does not contain topic: % x", packet)
	}
	if !bytes.Contains(packet, []byte(`{"ok":true}`)) {
		t.Fatalf("packet does not contain payload: % x", packet)
	}
}

func TestConnectPacketIncludesRetainedWill(t *testing.T) {
	packet := connectPacket("test-client", "", "", 30, willOptions{
		Topic:    "alphaess/status",
		Payload:  []byte("offline"),
		Retained: true,
	})

	if !bytes.Contains(packet, []byte{0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x26}) {
		t.Fatalf("connect packet does not contain expected flags: % x", packet)
	}
	if !bytes.Contains(packet, []byte("alphaess/status")) {
		t.Fatalf("connect packet does not contain will topic: % x", packet)
	}
	if !bytes.Contains(packet, []byte("offline")) {
		t.Fatalf("connect packet does not contain will payload: % x", packet)
	}
}

func TestClientPublishesToLocalBroker(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	published := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		connect := make([]byte, 1024)
		if _, err := conn.Read(connect); err != nil {
			return
		}
		conn.Write([]byte{0x20, 0x02, 0x00, 0x00})

		packet := make([]byte, 1024)
		n, err := conn.Read(packet)
		if err != nil && err != io.EOF {
			return
		}
		published <- packet[:n]
	}()

	client := NewClient(Options{
		Broker:   "tcp://" + listener.Addr().String(),
		ClientID: "test-client",
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if err := client.PublishJSON(context.Background(), "alphaess/raw/state", []byte(`{"ok":true}`), false); err != nil {
		t.Fatalf("PublishJSON() error = %v", err)
	}

	packet := <-published
	if packet[0] != 0x30 {
		t.Fatalf("publish header = %#x", packet[0])
	}
	if !bytes.Contains(packet, []byte("alphaess/raw/state")) {
		t.Fatalf("publish packet does not contain topic: % x", packet)
	}
}

func TestPublishJSONWritesFullPacket(t *testing.T) {
	conn := &chunkedConn{maxWrite: 3}
	client := &Client{conn: conn}

	if err := client.PublishJSON(context.Background(), "alphaess/raw/state", []byte(`{"ok":true}`), false); err != nil {
		t.Fatalf("PublishJSON() error = %v", err)
	}

	want := publishPacket("alphaess/raw/state", []byte(`{"ok":true}`), false)
	if !bytes.Equal(conn.written.Bytes(), want) {
		t.Fatalf("written packet = % x, want % x", conn.written.Bytes(), want)
	}
}

func TestPublishJSONReconnectsAfterWriteFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	published := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		connect := make([]byte, 1024)
		if _, err := conn.Read(connect); err != nil {
			return
		}
		if _, err := conn.Write([]byte{0x20, 0x02, 0x00, 0x00}); err != nil {
			return
		}

		packet := make([]byte, 1024)
		n, err := conn.Read(packet)
		if err != nil && err != io.EOF {
			return
		}
		published <- packet[:n]
	}()

	client := &Client{
		opts: Options{
			Broker:   "tcp://" + listener.Addr().String(),
			ClientID: "test-client",
		},
		conn: failingConn{},
	}
	if err := client.PublishJSON(context.Background(), "alphaess/raw/state", []byte(`{"ok":true}`), false); err != nil {
		t.Fatalf("PublishJSON() error = %v", err)
	}
	defer client.Close()

	packet := <-published
	if packet[0] != 0x30 {
		t.Fatalf("publish header = %#x", packet[0])
	}
	if !bytes.Contains(packet, []byte("alphaess/raw/state")) {
		t.Fatalf("publish packet does not contain topic: % x", packet)
	}
}

func TestConnectRequiresUsernameWithPassword(t *testing.T) {
	path := t.TempDir() + "/password"
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		accepted <- struct{}{}
	}()

	client := NewClient(Options{
		Broker:       "tcp://" + listener.Addr().String(),
		ClientID:     "test-client",
		PasswordFile: path,
	})
	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want username error")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("Connect() error = %v, want username error", err)
	}
	<-accepted
}

type chunkedConn struct {
	written  bytes.Buffer
	maxWrite int
}

func (c *chunkedConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (c *chunkedConn) Write(data []byte) (int, error) {
	if c.maxWrite <= 0 {
		return 0, errors.New("maxWrite must be positive")
	}
	if len(data) > c.maxWrite {
		data = data[:c.maxWrite]
	}
	return c.written.Write(data)
}

func (c *chunkedConn) Close() error {
	return nil
}

func (c *chunkedConn) LocalAddr() net.Addr {
	return dummyAddr("local")
}

func (c *chunkedConn) RemoteAddr() net.Addr {
	return dummyAddr("remote")
}

func (c *chunkedConn) SetDeadline(time.Time) error {
	return nil
}

func (c *chunkedConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *chunkedConn) SetWriteDeadline(time.Time) error {
	return nil
}

type dummyAddr string

func (a dummyAddr) Network() string {
	return string(a)
}

func (a dummyAddr) String() string {
	return string(a)
}

type failingConn struct{}

func (f failingConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (f failingConn) Write([]byte) (int, error) {
	return 0, errors.New("simulated write failure")
}

func (f failingConn) Close() error {
	return nil
}

func (f failingConn) LocalAddr() net.Addr {
	return dummyAddr("local")
}

func (f failingConn) RemoteAddr() net.Addr {
	return dummyAddr("remote")
}

func (f failingConn) SetDeadline(time.Time) error {
	return nil
}

func (f failingConn) SetReadDeadline(time.Time) error {
	return nil
}

func (f failingConn) SetWriteDeadline(time.Time) error {
	return nil
}
