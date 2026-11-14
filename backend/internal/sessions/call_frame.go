package sessions

import (
	"bytes"
	"encoding/gob"
	"errors"
	"io"
	"sync"
)

// A shard's frame is one gob stream: the definitions of callFrame's types,
// then the frame. Every frame is written by an Encoder of its own, so every
// frame sends the same definitions, and a Decoder of its own compiled them
// again for each: over ~15.6k shards at a start, ~400 MB of a cold Requests
// build's ~1 GB of reading them, and the collections that came with it
// (#3493). A shardDecoder keeps one Decoder and the definitions it has
// read, and hands it only the frame when a frame's definitions are those.

type shardDecoder struct {
	types []byte // the definitions dec has read, as they were sent
	in    frameInput
	dec   *gob.Decoder
}

// frameInput is a Decoder's input, swapped from frame to frame. It is an
// io.ByteReader, so the Decoder reads no further than each message.
type frameInput struct{ b []byte }

func (r *frameInput) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func (r *frameInput) ReadByte() (byte, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c, nil
}

var shardDecoders sync.Pool

var errShardFrame = errors.New("bad call frame")

// decodeFrame decodes the gob stream data into frame.
func decodeFrame(data []byte, frame *callFrame) error {
	types, ok := gobTypes(data)
	if !ok {
		return errShardFrame
	}
	fd, _ := shardDecoders.Get().(*shardDecoder)
	if fd != nil && bytes.Equal(fd.types, types) {
		fd.in.b = data[len(types):]
	} else {
		fd = &shardDecoder{types: bytes.Clone(types)}
		fd.in.b = data
		fd.dec = gob.NewDecoder(&fd.in)
	}
	// a Decoder that failed is let go: what it read is unknown
	if err := fd.dec.Decode(frame); err != nil {
		return err
	}
	if len(fd.in.b) != 0 {
		return errShardFrame
	}
	shardDecoders.Put(fd)
	return nil
}

// gobTypes is the start of the gob stream data that defines types: the
// messages before the first one of a value. Each message is its length,
// then its type id, negative for a definition.
func gobTypes(data []byte) ([]byte, bool) {
	at := 0
	for at < len(data) {
		n, k := gobUint(data[at:])
		if k == 0 || n > uint64(len(data)-at-k) {
			return nil, false
		}
		id, j := gobUint(data[at+k:])
		if j == 0 {
			return nil, false
		}
		if id&1 == 0 { // a positive id: a value
			return data[:at], true
		}
		at += k + int(n)
	}
	return nil, false
}

// gobUint reads one of gob's unsigned integers: a byte below 0x80 is the
// value, any other is the negated count of big-endian bytes that follow.
// k is how many bytes it took, 0 for a bad one.
func gobUint(b []byte) (v uint64, k int) {
	if len(b) == 0 {
		return 0, 0
	}
	if b[0] < 0x80 {
		return uint64(b[0]), 1
	}
	n := -int(int8(b[0]))
	if n > 8 || n >= len(b) {
		return 0, 0
	}
	for _, c := range b[1 : 1+n] {
		v = v<<8 | uint64(c)
	}
	return v, 1 + n
}
