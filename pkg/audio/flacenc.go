package audio

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"math/bits"
)

// tabula's own FLAC encoder, for compressing a session's tracks in place
// (compress.go): mono, 16 or 24 bits, frames of flacBlock samples, each
// subframe constant, verbatim or a fixed predictor (order 0-4) with a
// partitioned Rice residual; a SEEKTABLE every flacSeekEvery seconds so
// that replays and meters start anywhere; the MD5 of the samples in
// STREAMINFO. Close to `flac -1` in size, and fast: no LPC search.

const (
	flacBlock        = 4096
	flacMaxPartOrder = 6
	flacSeekEvery    = 2 // seconds between seek points
)

// flacWriter writes a mono FLAC stream to a file it can rewrite the header
// of (the length, the MD5 and the seek points are known at the end).
type flacWriter struct {
	w       io.WriterAt
	rate    int
	bps     int
	total   int64 // samples to write (sizes the seek table)
	off     int64 // where the next byte goes
	audio   int64 // where the first frame starts
	frame   uint64
	written int64
	md5     hash.Hash
	seeks   []flacSeekPoint
	minF    int
	maxF    int
	bw      bitWriter
	resid   []int64
	zz      []uint64
	md5b    []byte
}

type flacSeekPoint struct {
	sample int64
	offset int64 // from the first frame
	n      int   // samples in the frame there
}

// newFLACWriter starts a FLAC stream of total samples at rate, bps bits.
func newFLACWriter(w io.WriterAt, rate, bps int, total int64) (*flacWriter, error) {
	if bps != 16 && bps != 24 {
		return nil, fmt.Errorf("FLAC: %d bits per sample, 16 or 24 expected", bps)
	}
	if rate <= 0 || rate > 655350 {
		return nil, fmt.Errorf("FLAC: rate %d", rate)
	}
	f := &flacWriter{w: w, rate: rate, bps: bps, total: total, md5: md5.New(), minF: math.MaxInt}
	points := 0
	if every := int64(rate) * flacSeekEvery; total > 0 {
		points = int((total + every - 1) / every)
	}
	f.seeks = make([]flacSeekPoint, 0, points)
	// "fLaC", STREAMINFO, SEEKTABLE: written again at the end
	f.audio = 4 + 4 + 34 + 4 + int64(18*points)
	f.off = f.audio
	return f, nil
}

// writeBlock encodes up to flacBlock samples (signed, bps bits) as a frame.
func (f *flacWriter) writeBlock(s []int32) error {
	n := len(s)
	if n == 0 {
		return nil
	}
	// the seek point of every flacSeekEvery seconds is the frame holding it
	every := int64(f.rate) * flacSeekEvery
	if len(f.seeks) < cap(f.seeks) && f.written+int64(n) > int64(len(f.seeks))*every {
		f.seeks = append(f.seeks, flacSeekPoint{sample: f.written, offset: f.off - f.audio, n: n})
	}
	f.md5b = f.md5b[:0]
	for _, v := range s {
		if f.bps == 16 {
			f.md5b = append(f.md5b, byte(v), byte(v>>8))
		} else {
			f.md5b = append(f.md5b, byte(v), byte(v>>8), byte(v>>16))
		}
	}
	f.md5.Write(f.md5b)

	bw := &f.bw
	bw.reset()
	// frame header: sync, fixed blocksize, block size, rate from STREAMINFO, mono, sample size
	bw.write(0xFFF8, 16)
	code := uint64(0x7) // 16-bit (blocksize-1) at the end of the header
	if n == flacBlock {
		code = 0xC // 4096
	}
	bw.write(code, 4)
	bw.write(0, 4) // sample rate: STREAMINFO
	bw.write(0, 4) // mono
	if f.bps == 16 {
		bw.write(0x4, 3)
	} else {
		bw.write(0x6, 3)
	}
	bw.write(0, 1)
	writeUTF8(bw, f.frame)
	if code == 0x7 {
		bw.write(uint64(n-1), 16)
	}
	bw.flush() // the header is whole bytes
	bw.write(uint64(crc8(bw.buf)), 8)
	f.subframe(s)
	bw.align()
	crc := crc16(bw.buf)
	bw.write(uint64(crc), 16)
	bw.flush()

	if _, err := f.w.WriteAt(bw.buf, f.off); err != nil {
		return err
	}
	size := len(bw.buf)
	f.off += int64(size)
	f.minF, f.maxF = min(f.minF, size), max(f.maxF, size)
	f.frame++
	f.written += int64(n)
	return nil
}

// subframe writes the smallest of: constant, a fixed predictor with a
// Rice residual, verbatim.
func (f *flacWriter) subframe(s []int32) {
	bw, n, bps := &f.bw, len(s), f.bps
	constant := true
	for _, v := range s[1:] {
		if v != s[0] {
			constant = false
			break
		}
	}
	if constant {
		bw.write(0, 1)
		bw.write(0, 6) // CONSTANT
		bw.write(0, 1)
		bw.writeSigned(int64(s[0]), bps)
		return
	}
	// the fixed predictor whose residual is the smallest (libFLAC's choice)
	order := 0
	if n > 4 {
		var sums [5]uint64
		for i := 4; i < n; i++ {
			e0 := int64(s[i])
			e1 := e0 - int64(s[i-1])
			e2 := e1 - (int64(s[i-1]) - int64(s[i-2]))
			e3 := e2 - (int64(s[i-1]) - 2*int64(s[i-2]) + int64(s[i-3]))
			e4 := e3 - (int64(s[i-1]) - 3*int64(s[i-2]) + 3*int64(s[i-3]) - int64(s[i-4]))
			sums[0] += abs64(e0)
			sums[1] += abs64(e1)
			sums[2] += abs64(e2)
			sums[3] += abs64(e3)
			sums[4] += abs64(e4)
		}
		for o := 1; o < 5; o++ {
			if sums[o] < sums[order] {
				order = o
			}
		}
	}
	if cap(f.resid) < n {
		f.resid, f.zz = make([]int64, n), make([]uint64, n)
	}
	r, zz := f.resid[:n], f.zz[:n]
	for i := order; i < n; i++ {
		x := int64(s[i])
		switch order {
		case 0:
		case 1:
			x -= int64(s[i-1])
		case 2:
			x -= 2*int64(s[i-1]) - int64(s[i-2])
		case 3:
			x -= 3*int64(s[i-1]) - 3*int64(s[i-2]) + int64(s[i-3])
		case 4:
			x -= 4*int64(s[i-1]) - 6*int64(s[i-2]) + 4*int64(s[i-3]) - int64(s[i-4])
		}
		r[i] = x
		zz[i] = uint64((x << 1) ^ (x >> 63))
	}
	plan, ok := ricePlan(zz[order:], n, order)
	verbatim := 8 + n*bps
	if !ok || 8+order*bps+plan.bits >= verbatim {
		bw.write(0, 1)
		bw.write(1, 6) // VERBATIM
		bw.write(0, 1)
		for _, v := range s {
			bw.writeSigned(int64(v), bps)
		}
		return
	}
	bw.write(0, 1)
	bw.write(uint64(8|order), 6) // FIXED, order
	bw.write(0, 1)
	for i := range order {
		bw.writeSigned(int64(s[i]), bps)
	}
	method := uint64(0)
	pbits := 4
	if plan.rice2 {
		method, pbits = 1, 5
	}
	bw.write(method, 2)
	bw.write(uint64(plan.order), 4)
	parts := 1 << plan.order
	per := n >> plan.order
	i := order
	for p := range parts {
		end := (p + 1) * per
		k := plan.k[p]
		bw.write(uint64(k), pbits)
		for ; i < end; i++ {
			bw.writeRice(zz[i], k)
		}
	}
}

func abs64(x int64) uint64 {
	if x < 0 {
		return uint64(-x)
	}
	return uint64(x)
}

// A rice is how a residual is coded: a partition order, a Rice
// parameter per partition, the bits it takes.
type rice struct {
	order int
	k     [1 << flacMaxPartOrder]int
	bits  int
	rice2 bool
}

// ricePlan picks the partition order and parameters taking the fewest
// bits for zz, the zigzagged residual of a block of n samples whose first
// pred samples are warm-up (not coded). Sums of the finest partitions are
// merged upwards; a parameter is estimated from a partition's mean, its
// cost from its sum: close to the exact cost, at a fraction of the work.
func ricePlan(zz []uint64, n, pred int) (rice, bool) {
	maxOrder := 0
	for o := flacMaxPartOrder; o > 0; o-- {
		if n%(1<<o) == 0 && n>>o > pred {
			maxOrder = o
			break
		}
	}
	var sums [1 << flacMaxPartOrder]uint64
	parts := 1 << maxOrder
	per := n >> maxOrder
	for p := range parts {
		lo, hi := p*per-pred, (p+1)*per-pred
		lo = max(lo, 0)
		var sum uint64
		for _, u := range zz[lo:hi] {
			sum += u
		}
		sums[p] = sum
	}
	best := rice{bits: math.MaxInt}
	for o := maxOrder; o >= 0; o-- {
		parts := 1 << o
		per := n >> o
		var plan rice
		plan.order = o
		bitsUsed := 0
		for p := range parts {
			count := per
			if p == 0 {
				count -= pred
			}
			k, cost := riceParam(sums[p], count)
			plan.k[p] = k
			bitsUsed += cost
			plan.rice2 = plan.rice2 || k > 14
		}
		if plan.rice2 {
			bitsUsed += parts * 5
		} else {
			bitsUsed += parts * 4
		}
		plan.bits = 2 + 4 + bitsUsed
		if plan.bits < best.bits {
			best = plan
		}
		if o > 0 { // merge pairs for the next order down
			for p := range parts / 2 {
				sums[p] = sums[2*p] + sums[2*p+1]
			}
		}
	}
	for p := range 1 << best.order {
		if best.k[p] > 30 {
			return best, false
		}
	}
	return best, true
}

// riceParam estimates the Rice parameter of count values summing to sum,
// and the bits they take with it.
func riceParam(sum uint64, count int) (k, bits int) {
	if count <= 0 {
		return 0, 0
	}
	mean := sum / uint64(count)
	if mean > 0 {
		k = 63 - bitsLeadingZeros(mean)
	}
	return k, count*(k+1) + int(sum>>uint(k))
}

func bitsLeadingZeros(x uint64) int { return bits.LeadingZeros64(x) }

// close writes the last frames' header fields: STREAMINFO (length, frame
// sizes, MD5) and the SEEKTABLE, and checks the length.
func (f *flacWriter) close() error {
	if f.written != f.total {
		return fmt.Errorf("FLAC: %d samples written, %d announced", f.written, f.total)
	}
	head := make([]byte, 0, f.audio)
	head = append(head, "fLaC"...)
	last := byte(0)
	if cap(f.seeks) == 0 {
		last = 0x80
	}
	head = append(head, last|0, 0, 0, 34)
	var si [34]byte
	block := flacBlock
	if f.total < flacBlock {
		block = max(int(f.total), 16)
	}
	binary.BigEndian.PutUint16(si[0:], uint16(block))
	binary.BigEndian.PutUint16(si[2:], uint16(block))
	minF, maxF := f.minF, f.maxF
	if f.frame == 0 {
		minF, maxF = 0, 0
	}
	put24(si[4:], minF)
	put24(si[7:], maxF)
	// rate (20 bits), channels-1 (3), bps-1 (5), total samples (36)
	v := uint64(f.rate)<<44 | uint64(0)<<41 | uint64(f.bps-1)<<36 | uint64(f.total)&(1<<36-1)
	binary.BigEndian.PutUint64(si[10:], v)
	copy(si[18:], f.md5.Sum(nil))
	head = append(head, si[:]...)
	if n := cap(f.seeks); n > 0 {
		size := 18 * n
		head = append(head, 0x80|3, byte(size>>16), byte(size>>8), byte(size))
		for i := range n {
			var p [18]byte
			if i < len(f.seeks) {
				sp := f.seeks[i]
				binary.BigEndian.PutUint64(p[0:], uint64(sp.sample))
				binary.BigEndian.PutUint64(p[8:], uint64(sp.offset))
				binary.BigEndian.PutUint16(p[16:], uint16(sp.n))
			} else { // a placeholder point
				binary.BigEndian.PutUint64(p[0:], math.MaxUint64)
			}
			head = append(head, p[:]...)
		}
	}
	if int64(len(head)) != f.audio {
		return errors.New("FLAC: header size")
	}
	_, err := f.w.WriteAt(head, 0)
	return err
}

func put24(b []byte, v int) { b[0], b[1], b[2] = byte(v>>16), byte(v>>8), byte(v) }

// sum is the MD5 of the samples written.
func (f *flacWriter) sum() []byte { return f.md5.Sum(nil) }

// writeUTF8 writes a frame number in FLAC's UTF-8-like coding.
func writeUTF8(bw *bitWriter, v uint64) {
	switch {
	case v < 0x80:
		bw.write(v, 8)
		return
	}
	n := 2 // bytes
	for v >= 1<<(5*n+1) && n < 7 {
		n++
	}
	lead := uint64(0xFF<<(8-n)) & 0xFF
	bw.write(lead|v>>(6*(n-1)), 8)
	for i := n - 2; i >= 0; i-- {
		bw.write(0x80|(v>>(6*i))&0x3F, 8)
	}
}

// bitWriter writes bits, most significant first.
type bitWriter struct {
	buf []byte
	acc uint64
	n   uint // bits waiting in acc
}

func (w *bitWriter) reset() { w.buf, w.acc, w.n = w.buf[:0], 0, 0 }

func (w *bitWriter) write(v uint64, nbits int) {
	for nbits > 32 {
		w.write(v>>32, nbits-32)
		v &= 1<<32 - 1
		nbits = 32
	}
	w.acc = w.acc<<uint(nbits) | v&(1<<uint(nbits)-1)
	w.n += uint(nbits)
	if w.n >= 32 { // 32 bits at once: acc holds at most 63
		x := uint32(w.acc >> (w.n - 32))
		w.buf = append(w.buf, byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
		w.n -= 32
	}
}

// flush writes the whole bytes waiting.
func (w *bitWriter) flush() {
	for w.n >= 8 {
		w.buf = append(w.buf, byte(w.acc>>(w.n-8)))
		w.n -= 8
	}
}

func (w *bitWriter) writeSigned(v int64, nbits int) { w.write(uint64(v), nbits) }

// writeRice writes u with Rice parameter k: u>>k zeros, a one, k low bits.
func (w *bitWriter) writeRice(u uint64, k int) {
	q := u >> uint(k)
	if n := int(q) + 1 + k; n <= 32 { // the usual case: one write
		w.write(1<<uint(k)|u&(1<<uint(k)-1), n)
		return
	}
	for q >= 32 {
		w.write(0, 32)
		q -= 32
	}
	w.write(1, int(q)+1)
	if k > 0 {
		w.write(u, k)
	}
}

func (w *bitWriter) align() {
	if w.n%8 != 0 {
		w.write(0, int(8-w.n%8))
	}
	w.flush()
}

// crc16By4 are the tables of a byte followed by 0-3 zero bytes, for
// CRC-16 four bytes at a time.
var crc16By4 = func() (t [4][256]uint16) {
	t[0] = crc16Table
	for k := 1; k < 4; k++ {
		for i := range 256 {
			c := t[k-1][i]
			t[k][i] = c<<8 ^ crc16Table[byte(c>>8)]
		}
	}
	return
}()

var crc8Table, crc16Table = func() (t8 [256]byte, t16 [256]uint16) {
	for i := range 256 {
		c := byte(i)
		for range 8 {
			if c&0x80 != 0 {
				c = c<<1 ^ 0x07
			} else {
				c <<= 1
			}
		}
		t8[i] = c
		d := uint16(i) << 8
		for range 8 {
			if d&0x8000 != 0 {
				d = d<<1 ^ 0x8005
			} else {
				d <<= 1
			}
		}
		t16[i] = d
	}
	return
}()

func crc8(b []byte) byte {
	var c byte
	for _, x := range b {
		c = crc8Table[c^x]
	}
	return c
}

func crc16(b []byte) uint16 {
	var c uint16
	t := &crc16By4
	for len(b) >= 4 {
		x := c ^ (uint16(b[0])<<8 | uint16(b[1]))
		c = t[3][x>>8] ^ t[2][x&0xFF] ^ t[1][b[2]] ^ t[0][b[3]]
		b = b[4:]
	}
	for _, x := range b {
		c = c<<8 ^ crc16Table[byte(c>>8)^x]
	}
	return c
}
