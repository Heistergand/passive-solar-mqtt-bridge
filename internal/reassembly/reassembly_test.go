package reassembly

import (
	"bytes"
	"testing"
)

func TestAssemblerPushesInOrderSegments(t *testing.T) {
	assembler := NewAssembler()

	chunks := assembler.Push(Segment{FlowID: "flow", Seq: 100, Data: []byte("hello")})

	if got := joinChunks(chunks); string(got) != "hello" {
		t.Fatalf("chunks = %q, want hello", got)
	}
}

func TestAssemblerBuffersOutOfOrderSegments(t *testing.T) {
	assembler := NewAssembler()

	if chunks := assembler.Push(Segment{FlowID: "flow", Seq: 105, Data: []byte("world")}); len(chunks) != 1 {
		t.Fatalf("first segment chunks = %d, want 1 because first segment starts the stream", len(chunks))
	}

	assembler = NewAssembler()
	chunks := assembler.Push(Segment{FlowID: "flow", Seq: 100, Data: []byte("hello")})
	if got := joinChunks(chunks); string(got) != "hello" {
		t.Fatalf("first chunks = %q, want hello", got)
	}

	chunks = assembler.Push(Segment{FlowID: "flow", Seq: 110, Data: []byte("!")})
	if len(chunks) != 0 {
		t.Fatalf("out-of-order chunks = %d, want 0", len(chunks))
	}

	chunks = assembler.Push(Segment{FlowID: "flow", Seq: 105, Data: []byte("world")})
	if got := joinChunks(chunks); string(got) != "world!" {
		t.Fatalf("drained chunks = %q, want world!", got)
	}
}

func TestAssemblerIgnoresDuplicateSegment(t *testing.T) {
	assembler := NewAssembler()

	assembler.Push(Segment{FlowID: "flow", Seq: 100, Data: []byte("hello")})
	chunks := assembler.Push(Segment{FlowID: "flow", Seq: 100, Data: []byte("hello")})

	if len(chunks) != 0 {
		t.Fatalf("duplicate chunks = %d, want 0", len(chunks))
	}
}

func TestAssemblerTrimsOverlappingRetransmission(t *testing.T) {
	assembler := NewAssembler()

	assembler.Push(Segment{FlowID: "flow", Seq: 100, Data: []byte("hello")})
	chunks := assembler.Push(Segment{FlowID: "flow", Seq: 103, Data: []byte("lo world")})

	if got := joinChunks(chunks); string(got) != " world" {
		t.Fatalf("overlap chunks = %q, want ' world'", got)
	}
}

func TestAssemblerKeepsFlowsSeparate(t *testing.T) {
	assembler := NewAssembler()

	a := assembler.Push(Segment{FlowID: "a", Seq: 100, Data: []byte("a1")})
	b := assembler.Push(Segment{FlowID: "b", Seq: 100, Data: []byte("b1")})

	if got := joinChunks(a); string(got) != "a1" {
		t.Fatalf("flow a chunks = %q", got)
	}
	if got := joinChunks(b); string(got) != "b1" {
		t.Fatalf("flow b chunks = %q", got)
	}
}

func joinChunks(chunks []StreamChunk) []byte {
	var buf bytes.Buffer
	for _, chunk := range chunks {
		buf.Write(chunk.Data)
	}
	return buf.Bytes()
}
