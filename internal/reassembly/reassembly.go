package reassembly

type Segment struct {
	FlowID string
	Seq    uint32
	Data   []byte
}

type StreamChunk struct {
	FlowID string
	Data   []byte
}

type Assembler struct {
	flows map[string]*flowState
}

type flowState struct {
	nextSeq   uint32
	started   bool
	buffered  map[uint32][]byte
	bytesRead int
}

func NewAssembler() *Assembler {
	return &Assembler{
		flows: map[string]*flowState{},
	}
}

func (a *Assembler) Push(segment Segment) []StreamChunk {
	if len(segment.Data) == 0 {
		return nil
	}

	state := a.flow(segment.FlowID)
	if !state.started {
		state.started = true
		state.nextSeq = segment.Seq
	}

	chunks := state.push(segment)
	if len(chunks) == 0 {
		return nil
	}

	out := make([]StreamChunk, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, StreamChunk{
			FlowID: segment.FlowID,
			Data:   chunk,
		})
	}
	return out
}

func (a *Assembler) flow(flowID string) *flowState {
	state, ok := a.flows[flowID]
	if ok {
		return state
	}

	state = &flowState{
		buffered: map[uint32][]byte{},
	}
	a.flows[flowID] = state
	return state
}

func (s *flowState) push(segment Segment) [][]byte {
	seq := segment.Seq
	data := cloneBytes(segment.Data)

	if seqBefore(seq, s.nextSeq) {
		alreadyRead := seqDistance(seq, s.nextSeq)
		if alreadyRead >= uint32(len(data)) {
			return nil
		}
		data = data[alreadyRead:]
		seq = s.nextSeq
	}

	if seq != s.nextSeq {
		if _, exists := s.buffered[seq]; !exists {
			s.buffered[seq] = data
		}
		return nil
	}

	var chunks [][]byte
	chunks = append(chunks, data)
	s.advance(uint32(len(data)))

	for {
		next, ok := s.buffered[s.nextSeq]
		if !ok {
			break
		}
		delete(s.buffered, s.nextSeq)
		chunks = append(chunks, next)
		s.advance(uint32(len(next)))
	}

	return chunks
}

func (s *flowState) advance(length uint32) {
	s.nextSeq += length
	s.bytesRead += int(length)
}

func seqBefore(a, b uint32) bool {
	return int32(a-b) < 0
}

func seqDistance(from, to uint32) uint32 {
	return to - from
}

func cloneBytes(data []byte) []byte {
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return cloned
}
