package alphaess

import (
	"bytes"
	"encoding/json"
)

const maxBufferedBytes = 1 << 20
const frameHeaderBytes = 7
const frameChecksumBytes = 2
const maxFramePayloadBytes = 1 << 16

type Message struct {
	RawJSON []byte
	Fields  map[string]any
}

type Decoder struct {
	buffer []byte
}

func NewDecoder() *Decoder {
	return &Decoder{}
}

func (d *Decoder) Push(data []byte) ([]Message, error) {
	d.buffer = append(d.buffer, data...)

	var messages []Message
	for {
		if len(d.buffer) >= 2 && d.buffer[0] == 0x01 && d.buffer[1] == 0x01 {
			frameMessages, complete := d.consumeFrame()
			messages = append(messages, frameMessages...)
			if !complete {
				return messages, nil
			}
			continue
		}

		if d.alignToNextRecordStart() {
			continue
		}

		if len(d.buffer) == 0 {
			return messages, nil
		}

		if d.buffer[0] != '{' {
			d.discardOversizedGarbage()
			return messages, nil
		}

		end, complete := findJSONObjectEnd(d.buffer)
		if !complete {
			d.discardOversizedGarbage()
			return messages, nil
		}

		raw := cloneBytes(d.buffer[:end])
		d.buffer = d.buffer[end:]

		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			continue
		}
		messages = append(messages, Message{
			RawJSON: raw,
			Fields:  fields,
		})
	}
}

func (d *Decoder) consumeFrame() ([]Message, bool) {
	if len(d.buffer) < frameHeaderBytes {
		return nil, false
	}

	payloadLen := int(d.buffer[3])<<24 | int(d.buffer[4])<<16 | int(d.buffer[5])<<8 | int(d.buffer[6])
	if payloadLen < 0 || payloadLen > maxFramePayloadBytes {
		d.buffer = d.buffer[1:]
		return nil, true
	}

	frameLen := frameHeaderBytes + payloadLen + frameChecksumBytes
	if len(d.buffer) < frameLen {
		return nil, false
	}

	frame := d.buffer[:frameLen]
	d.buffer = d.buffer[frameLen:]

	expected := uint16(frame[frameLen-2])<<8 | uint16(frame[frameLen-1])
	if crc16Modbus(frame[:frameLen-2]) != expected {
		return nil, true
	}

	payload := cloneBytes(frame[frameHeaderBytes : frameHeaderBytes+payloadLen])
	if len(payload) == 0 {
		return nil, true
	}

	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, true
	}

	return []Message{{
		RawJSON: payload,
		Fields:  fields,
	}}, true
}

func (d *Decoder) alignToNextRecordStart() bool {
	frameStart := bytes.Index(d.buffer, []byte{0x01, 0x01})
	jsonStart := bytes.IndexByte(d.buffer, '{')

	switch {
	case frameStart == 0 || jsonStart == 0:
		return false
	case frameStart < 0 && jsonStart < 0:
		d.discardOversizedGarbage()
		return false
	case frameStart >= 0 && (jsonStart < 0 || frameStart < jsonStart):
		d.buffer = d.buffer[frameStart:]
		return true
	case jsonStart > 0:
		d.buffer = d.buffer[jsonStart:]
		return true
	default:
		return false
	}
}

func (d *Decoder) discardOversizedGarbage() {
	if len(d.buffer) <= maxBufferedBytes {
		return
	}

	start := bytes.LastIndexByte(d.buffer, '{')
	if start >= 0 {
		d.buffer = d.buffer[start:]
		return
	}
	d.buffer = d.buffer[:0]
}

func findJSONObjectEnd(data []byte) (int, bool) {
	if len(data) == 0 || data[0] != '{' {
		return 0, false
	}

	depth := 0
	inString := false
	escaped := false

	for i, b := range data {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch b {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}

	return 0, false
}

func crc16Modbus(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func cloneBytes(data []byte) []byte {
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return cloned
}
