package capture

import "context"

type Packet struct {
	TimestampUnixNano int64
	Data              []byte
}

type Source interface {
	Run(ctx context.Context, packets chan<- Packet) error
}
