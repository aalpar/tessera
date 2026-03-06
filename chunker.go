package tessera

import (
	"io"
	"iter"
)

// Rabin fingerprint parameters.
// The polynomial is an irreducible polynomial over GF(2).
// Window size determines how many bytes participate in the rolling hash.
const (
	rabinPoly   uint64 = 0x3DA3358B4DC173 // irreducible polynomial (degree 53)
	rabinWindow        = 48               // sliding window size in bytes
)

// ChunkerConfig controls content-defined chunk boundaries.
type ChunkerConfig struct {
	MinSize    int // minimum chunk size (bytes)
	MaxSize    int // maximum chunk size (bytes)
	TargetSize int // target average chunk size (bytes)
}

// DefaultChunkerConfig returns a reasonable default configuration.
// Target 8KB chunks with 2KB minimum and 32KB maximum.
func DefaultChunkerConfig() ChunkerConfig {
	return ChunkerConfig{
		MinSize:    2 * 1024,
		MaxSize:    32 * 1024,
		TargetSize: 8 * 1024,
	}
}

// Chunker splits data into content-defined chunks using Rabin fingerprinting.
type Chunker struct {
	cfg  ChunkerConfig
	mask uint64 // fingerprint mask derived from TargetSize

	// Precomputed tables for O(1) sliding window updates.
	popTable [256]uint64 // contribution of the byte leaving the window
}

// NewChunker creates a chunker with the given configuration.
func NewChunker(cfg ChunkerConfig) *Chunker {
	// mask has enough low bits set so that fingerprint & mask == 0
	// occurs on average every TargetSize bytes.
	// TargetSize should be a power of 2 for clean mask; we round down.
	bits := 0
	for t := cfg.TargetSize; t > 1; t >>= 1 {
		bits++
	}
	mask := uint64(1<<bits) - 1

	c := &Chunker{
		cfg:  cfg,
		mask: mask,
	}
	c.buildPopTable()
	return c
}

// buildPopTable precomputes the contribution of each byte value
// shifted out of the sliding window.
func (c *Chunker) buildPopTable() {
	// The byte leaving the window was multiplied by x^(8*windowSize)
	// modulo the polynomial. Precompute this for each possible byte.
	for b := range 256 {
		var h uint64
		h = appendByte(h, byte(b))
		for range (rabinWindow - 1) * 8 {
			h = shiftBit(h)
		}
		c.popTable[b] = h
	}
}

// appendByte adds 8 bits to the fingerprint, one at a time.
func appendByte(fp uint64, b byte) uint64 {
	for i := 7; i >= 0; i-- {
		fp = shiftBit(fp)
		if b&(1<<uint(i)) != 0 {
			fp ^= 1
		}
	}
	return fp
}

// shiftBit shifts the polynomial left by one bit, reducing modulo rabinPoly.
func shiftBit(fp uint64) uint64 {
	// If the high bit (degree 53) is set, we need to reduce.
	carry := fp & (1 << 53)
	fp <<= 1
	if carry != 0 {
		fp ^= rabinPoly
	}
	return fp
}

// Chunks returns an iterator over content-defined chunks from the reader.
func (c *Chunker) Chunks(r io.Reader) iter.Seq[Chunk] {
	return func(yield func(Chunk) bool) {
		buf := make([]byte, c.cfg.MaxSize)
		window := make([]byte, rabinWindow)
		wpos := 0   // position in circular window buffer
		wfill := 0  // how many bytes in the window so far (up to rabinWindow)
		var fp uint64
		n := 0 // bytes in current chunk (in buf)

		for {
			// Read one byte at a time from the reader.
			// For efficiency, we read a batch into a read buffer.
			var readBuf [4096]byte
			nr, err := r.Read(readBuf[:])

			for i := range nr {
				b := readBuf[i]
				buf[n] = b
				n++

				// Update rolling hash.
				if wfill >= rabinWindow {
					// Remove outgoing byte.
					outgoing := window[wpos]
					fp ^= c.popTable[outgoing]
				} else {
					wfill++
				}
				fp = appendByte(fp, b)
				window[wpos] = b
				wpos = (wpos + 1) % rabinWindow

				// Check for chunk boundary.
				if n >= c.cfg.MinSize && (fp&c.mask == 0 || n >= c.cfg.MaxSize) {
					if !yield(newChunk(buf[:n])) {
						return
					}
					n = 0
					fp = 0
					wfill = 0
					wpos = 0
				}
			}

			if err != nil {
				if err != io.EOF {
					// On read error, emit what we have and stop.
					// Caller can detect short reads from context.
				}
				break
			}
		}

		// Emit final chunk if any data remains.
		if n > 0 {
			yield(newChunk(buf[:n]))
		}
	}
}
