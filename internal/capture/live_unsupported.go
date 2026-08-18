//go:build !linux

package capture

import (
	"context"
	"errors"
	"io"

	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/homeassistant"
	"github.com/Heistergand/passive-solar-mqtt-bridge/internal/mqtt"
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

func ReadLive(context.Context, LiveOptions) (FileSummary, error) {
	return FileSummary{}, errors.New("live capture is only supported on Linux")
}
