package alphaess

import "testing"

func TestDecoderExtractsSingleJSONWithPrefixAndTrailer(t *testing.T) {
	decoder := NewDecoder()

	messages, err := decoder.Push([]byte("\x00\x01abc{\"SOC\":\"33.2\"}\r\n"))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["SOC"] != "33.2" {
		t.Fatalf("SOC = %v", messages[0].Fields["SOC"])
	}
}

func TestDecoderExtractsFramedJSONWithChecksum(t *testing.T) {
	decoder := NewDecoder()
	frame := alphaESSFrame(0x10, []byte(`{"SOC":"33.2","Ppv1":"10"}`))

	messages, err := decoder.Push(frame)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["SOC"] != "33.2" {
		t.Fatalf("SOC = %v", messages[0].Fields["SOC"])
	}
}

func TestDecoderWaitsForFragmentedFrame(t *testing.T) {
	decoder := NewDecoder()
	frame := alphaESSFrame(0x10, []byte(`{"SOC":"33.2"}`))

	messages, err := decoder.Push(frame[:5])
	if err != nil {
		t.Fatalf("Push() first error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("first messages = %d, want 0", len(messages))
	}

	messages, err = decoder.Push(frame[5:])
	if err != nil {
		t.Fatalf("Push() second error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("second messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["SOC"] != "33.2" {
		t.Fatalf("SOC = %v", messages[0].Fields["SOC"])
	}
}

func TestDecoderSkipsFrameWithInvalidChecksum(t *testing.T) {
	decoder := NewDecoder()
	bad := alphaESSFrame(0x10, []byte(`{"SOC":"33.2"}`))
	bad[len(bad)-1] ^= 0xff
	good := alphaESSFrame(0x10, []byte(`{"SOC":"33.4"}`))

	messages, err := decoder.Push(append(bad, good...))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["SOC"] != "33.4" {
		t.Fatalf("SOC = %v", messages[0].Fields["SOC"])
	}
}

func TestDecoderAcceptsObservedAlphaESSFrame(t *testing.T) {
	decoder := NewDecoder()
	frame := alphaESSFrame(0x10, []byte(`{"Time":"2026/08/15 01:55:20","SN":"ALB000000000000","Ppv1":"0","Ppv2":"0","Ppv3":"0","Ppv4":"0","PrealL1":"189","PrealL2":"200","PrealL3":"211","PmeterL1":"0","PmeterL2":"0","PmeterL3":"0","PmeterDC":"0","PmeterDCL1":"0","PmeterDCL2":"0","PmeterDCL3":"0","Pbat":"626","SOC":"32.8","GCPower":"0","UPSModel":"0","SYSMode":"11","EVCGunLockFlag":"0","Sva":"0","VarAC":"0","VarDC":"0"}`))

	messages, err := decoder.Push(frame)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["SOC"] != "32.8" {
		t.Fatalf("SOC = %v", messages[0].Fields["SOC"])
	}
	if messages[0].Fields["SN"] != "ALB000000000000" {
		t.Fatalf("SN = %v", messages[0].Fields["SN"])
	}
}

func TestDecoderWaitsForFragmentedJSON(t *testing.T) {
	decoder := NewDecoder()

	messages, err := decoder.Push([]byte("xx{\"Time\":\"2026/08/15"))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages = %d, want 0", len(messages))
	}

	messages, err = decoder.Push([]byte(" 01:52:16\",\"Ppv1\":\"0\"}yy"))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["Ppv1"] != "0" {
		t.Fatalf("Ppv1 = %v", messages[0].Fields["Ppv1"])
	}
}

func TestDecoderExtractsMultipleJSONObjects(t *testing.T) {
	decoder := NewDecoder()

	messages, err := decoder.Push([]byte(`{"a":"1"}xx{"b":"2"}`))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Fields["a"] != "1" {
		t.Fatalf("first field = %v", messages[0].Fields["a"])
	}
	if messages[1].Fields["b"] != "2" {
		t.Fatalf("second field = %v", messages[1].Fields["b"])
	}
}

func TestDecoderIgnoresBracesInsideStrings(t *testing.T) {
	decoder := NewDecoder()

	messages, err := decoder.Push([]byte(`{"text":"brace } inside","nested":{"ok":true}}`))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["text"] != "brace } inside" {
		t.Fatalf("text = %v", messages[0].Fields["text"])
	}
}

func TestDecoderSkipsInvalidJSONAndContinues(t *testing.T) {
	decoder := NewDecoder()

	messages, err := decoder.Push([]byte(`{invalid}xx{"ok":"yes"}`))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Fields["ok"] != "yes" {
		t.Fatalf("ok = %v", messages[0].Fields["ok"])
	}
}

func alphaESSFrame(kind byte, payload []byte) []byte {
	frame := []byte{
		0x01,
		0x01,
		kind,
		byte(len(payload) >> 24),
		byte(len(payload) >> 16),
		byte(len(payload) >> 8),
		byte(len(payload)),
	}
	frame = append(frame, payload...)
	checksum := crc16Modbus(frame)
	frame = append(frame, byte(checksum>>8), byte(checksum))
	return frame
}
