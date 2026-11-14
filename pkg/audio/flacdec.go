package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
)

// tabula's FLAC decoder, for the tracks of a compressed session: any mono
// FLAC stream (constant, verbatim, fixed and LPC subframes, wasted bits,
// both Rice codings), the frame CRCs checked; seeks go through the
// SEEKTABLE, else from the start.

type flacInfo struct {
	rate, bps int
	channels  int
	total     int64
	md5       [16]byte
	maxBlock  int
	maxFrame  int // bytes, 0 if unknown
}

type flacReader struct {
	f     *os.File
	br    *bufio.Reader
	info  flacInfo
	audio int64 // where the first frame starts
	seeks []flacSeekPoint

	pos   int64   // the sample Read gives next
	block []int32 // the frame decoded, starting at sample blockAt
	at    int64
	next  int64 // the sample the stream is at (after block)

	// the frame being decoded: its bytes (peeked), the bit read next
	fb   []byte
	bp   uint
	peek int // the most a frame takes
}

// openFLAC opens a mono FLAC file.
func openFLAC(path string) (*flacReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &flacReader{f: f, br: bufio.NewReaderSize(f, 64<<10)}
	if err := r.readHeader(); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

func (r *flacReader) readHeader() error {
	var magic [4]byte
	if _, err := io.ReadFull(r.br, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return errors.New("not a FLAC file")
	}
	off := int64(4)
	for last := false; !last; {
		var h [4]byte
		if _, err := io.ReadFull(r.br, h[:]); err != nil {
			return err
		}
		last = h[0]&0x80 != 0
		size := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		body := make([]byte, size)
		if _, err := io.ReadFull(r.br, body); err != nil {
			return err
		}
		off += 4 + int64(size)
		switch h[0] & 0x7F {
		case 0: // STREAMINFO
			if size < 34 {
				return errors.New("short STREAMINFO")
			}
			v := binary.BigEndian.Uint64(body[10:])
			r.info = flacInfo{maxBlock: int(binary.BigEndian.Uint16(body[2:])), maxFrame: int(body[7])<<16 | int(body[8])<<8 | int(body[9]), rate: int(v >> 44),
				channels: int(v>>41&7) + 1, bps: int(v>>36&31) + 1, total: int64(v & (1<<36 - 1))}
			copy(r.info.md5[:], body[18:34])
		case 3: // SEEKTABLE
			for i := 0; i+18 <= size; i += 18 {
				s := binary.BigEndian.Uint64(body[i:])
				if s == 1<<64-1 {
					continue
				}
				r.seeks = append(r.seeks, flacSeekPoint{sample: int64(s), offset: int64(binary.BigEndian.Uint64(body[i+8:])),
					n: int(binary.BigEndian.Uint16(body[i+16:]))})
			}
		}
	}
	switch {
	case r.info.rate == 0:
		return errors.New("no STREAMINFO")
	case r.info.channels != 1:
		return fmt.Errorf("%d channels, expected a mono track", r.info.channels)
	case r.info.bps > 32:
		return fmt.Errorf("%d bits per sample", r.info.bps)
	}
	r.audio = off
	// a frame is at most its samples verbatim (an encoder could do worse:
	// twice that), or what STREAMINFO says
	r.peek = 64 + 2*max(r.info.maxBlock, 4096)*r.info.bps/8
	if r.info.maxFrame > 0 {
		r.peek = r.info.maxFrame
	}
	if r.peek > r.br.Size() {
		r.br = bufio.NewReaderSize(r.f, r.peek+4096)
		if _, err := r.f.Seek(off, io.SeekStart); err != nil {
			return err
		}
	}
	return nil
}

func (r *flacReader) Rate() int     { return r.info.rate }
func (r *flacReader) Frames() int64 { return r.info.total }
func (r *flacReader) Close() error  { return r.f.Close() }

// SeekFrame moves to sample n: decoding on from where the stream is when n
// is ahead, else from the seek point before it (nothing is decoded again
// when n is in the frame decoded).
func (r *flacReader) SeekFrame(n int64) error {
	n = min(max(n, 0), r.info.total)
	r.pos = n
	if r.block != nil && n >= r.at && n < r.at+int64(len(r.block)) {
		return nil
	}
	start, off := int64(0), int64(0)
	for _, p := range r.seeks {
		if p.sample <= n {
			start, off = p.sample, p.offset
		}
	}
	if n < r.next || start > r.next { // behind the stream, or a point nearer
		if _, err := r.f.Seek(r.audio+off, io.SeekStart); err != nil {
			return err
		}
		r.br.Reset(r.f)
		r.next, r.at, r.block = start, start, nil
	}
	for r.next <= n && r.next < r.info.total {
		if err := r.decodeFrame(); err != nil {
			return err
		}
	}
	return nil
}

// Read gives the samples from the position on, full scale at ±2³¹ (as
// sampleFormat.sample does).
func (r *flacReader) Read(dst []int32) (int, error) {
	got := 0
	shift := uint(32 - r.info.bps)
	for got < len(dst) {
		if r.pos >= r.info.total {
			if got == 0 {
				return 0, io.EOF
			}
			break
		}
		if r.block == nil || r.pos < r.at || r.pos >= r.at+int64(len(r.block)) {
			if err := r.SeekFrame(r.pos); err != nil {
				return got, err
			}
			if r.pos >= r.at+int64(len(r.block)) { // the stream ended early
				return got, io.ErrUnexpectedEOF
			}
		}
		i := int(r.pos - r.at)
		n := copy(dst[got:], r.block[i:])
		for k := got; k < got+n; k++ {
			dst[k] <<= shift
		}
		got += n
		r.pos += int64(n)
	}
	return got, nil
}

// decodeFrame decodes the frame at the stream's position into block.
func (r *flacReader) decodeFrame() error {
	fb, err := r.br.Peek(r.peek)
	if len(fb) == 0 {
		if err == nil || err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	r.fb, r.bp = fb, 0
	sync, err := r.bits(16)
	if err != nil {
		return err
	}
	if sync&0xFFFE != 0xFFF8 {
		return fmt.Errorf("FLAC: no frame at sample %d", r.next)
	}
	bsCode, _ := r.bits(4)
	rateCode, _ := r.bits(4)
	chans, _ := r.bits(4)
	sizeCode, _ := r.bits(3)
	r.bits(1)
	if chans != 0 {
		return errors.New("FLAC: not a mono frame")
	}
	if err := r.skipUTF8(); err != nil {
		return err
	}
	n := 0
	switch {
	case bsCode == 1:
		n = 192
	case bsCode >= 2 && bsCode <= 5:
		n = 576 << (bsCode - 2)
	case bsCode == 6:
		v, _ := r.bits(8)
		n = int(v) + 1
	case bsCode == 7:
		v, _ := r.bits(16)
		n = int(v) + 1
	case bsCode >= 8:
		n = 256 << (bsCode - 8)
	default:
		return errors.New("FLAC: reserved block size")
	}
	switch rateCode {
	case 12:
		r.bits(8)
	case 13, 14:
		r.bits(16)
	}
	bps := r.info.bps
	if sizes := [8]int{0, 8, 12, 0, 16, 20, 24, 32}; sizeCode != 0 {
		bps = sizes[sizeCode]
		if bps == 0 {
			return errors.New("FLAC: reserved sample size")
		}
	}
	want := crc8(r.fb[:r.bp/8])
	got, err := r.bits(8)
	if err != nil {
		return err
	}
	if byte(got) != want {
		return fmt.Errorf("FLAC: bad header CRC at sample %d", r.next)
	}
	if cap(r.block) < n {
		r.block = make([]int32, n)
	}
	r.block = r.block[:n]
	if err := r.subframe(r.block, bps); err != nil {
		return err
	}
	r.bp = (r.bp + 7) &^ 7 // byte alignment
	want16 := crc16(r.fb[:r.bp/8])
	c, err := r.bits(16)
	if err != nil {
		return err
	}
	if uint16(c) != want16 {
		return fmt.Errorf("FLAC: bad frame CRC at sample %d", r.next)
	}
	if _, err := r.br.Discard(int(r.bp / 8)); err != nil {
		return err
	}
	r.at = r.next
	r.next += int64(n)
	if bps != r.info.bps { // samples are scaled by the stream's size
		for i := range r.block {
			r.block[i] <<= uint(r.info.bps - bps)
		}
	}
	return nil
}

func (r *flacReader) subframe(out []int32, bps int) error {
	if pad, err := r.bits(1); err != nil || pad != 0 {
		return errors.New("FLAC: bad subframe")
	}
	typ, _ := r.bits(6)
	wasted := 0
	if w, _ := r.bits(1); w == 1 {
		k, err := r.unary()
		if err != nil {
			return err
		}
		wasted = int(k) + 1
		bps -= wasted
	}
	n := len(out)
	switch {
	case typ == 0:
		v, err := r.signed(bps)
		if err != nil {
			return err
		}
		for i := range out {
			out[i] = int32(v)
		}
	case typ == 1:
		for i := range out {
			v, err := r.signed(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
	case typ >= 8 && typ <= 12:
		order := int(typ - 8)
		if order > n {
			return errors.New("FLAC: bad predictor order")
		}
		for i := range order {
			v, err := r.signed(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
		if err := r.residual(out, order); err != nil {
			return err
		}
		for i := order; i < n; i++ {
			var p int64
			switch order {
			case 1:
				p = int64(out[i-1])
			case 2:
				p = 2*int64(out[i-1]) - int64(out[i-2])
			case 3:
				p = 3*int64(out[i-1]) - 3*int64(out[i-2]) + int64(out[i-3])
			case 4:
				p = 4*int64(out[i-1]) - 6*int64(out[i-2]) + 4*int64(out[i-3]) - int64(out[i-4])
			}
			out[i] = int32(int64(out[i]) + p)
		}
	case typ >= 32:
		order := int(typ-32) + 1
		if order > n {
			return errors.New("FLAC: bad LPC order")
		}
		for i := range order {
			v, err := r.signed(bps)
			if err != nil {
				return err
			}
			out[i] = int32(v)
		}
		prec, _ := r.bits(4)
		if prec == 15 {
			return errors.New("FLAC: bad LPC precision")
		}
		shift, err := r.signed(5)
		if err != nil {
			return err
		}
		coefs := make([]int64, order)
		for i := range coefs {
			c, err := r.signed(int(prec) + 1)
			if err != nil {
				return err
			}
			coefs[i] = c
		}
		if err := r.residual(out, order); err != nil {
			return err
		}
		for i := order; i < n; i++ {
			var sum int64
			for j, c := range coefs {
				sum += c * int64(out[i-1-j])
			}
			out[i] = int32(int64(out[i]) + sum>>uint(max(shift, 0)))
		}
	default:
		return fmt.Errorf("FLAC: reserved subframe type %d", typ)
	}
	if wasted > 0 {
		for i := range out {
			out[i] <<= uint(wasted)
		}
	}
	return nil
}

func (r *flacReader) residual(out []int32, pred int) error {
	method, _ := r.bits(2)
	pbits := uint(4)
	switch method {
	case 0:
	case 1:
		pbits = 5
	default:
		return errors.New("FLAC: reserved residual coding")
	}
	order, _ := r.bits(4)
	parts := 1 << order
	n := len(out)
	if n%parts != 0 || n>>order < pred {
		return errors.New("FLAC: bad partition order")
	}
	per := n >> order
	i := pred
	escape := uint64(1)<<pbits - 1
	for p := range parts {
		end := (p + 1) * per
		k, err := r.bits(pbits)
		if err != nil {
			return err
		}
		if k == escape {
			size, _ := r.bits(5)
			for ; i < end; i++ {
				v, err := r.signed(int(size))
				if err != nil {
					return err
				}
				out[i] = int32(v)
			}
			continue
		}
		kk := uint(k)
		limit := uint(len(r.fb)) * 8
		for ; i < end; i++ {
			// the usual case: the zeros, the one and the k bits in one load
			x := r.load()
			z := uint(bits.LeadingZeros64(x))
			if n := z + 1 + kk; n <= 64-(r.bp&7) && r.bp+n <= limit {
				u := uint64(z)<<kk | x<<(z+1)>>(64-kk)
				if kk == 0 {
					u = uint64(z)
				}
				r.bp += n
				out[i] = int32(int64(u>>1) ^ -int64(u&1))
				continue
			}
			q, err := r.unary()
			if err != nil {
				return err
			}
			low, err := r.bits(kk)
			if err != nil {
				return err
			}
			u := q<<kk | low
			out[i] = int32(int64(u>>1) ^ -int64(u&1))
		}
	}
	return nil
}

// bits reads n bits (up to 32) of the frame, most significant first.
func (r *flacReader) bits(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if r.bp+n > uint(len(r.fb))*8 {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.load() >> (64 - n)
	r.bp += n
	return v, nil
}

// load returns the 64 bits from the bit position on (zeros past the end).
func (r *flacReader) load() uint64 {
	i := r.bp >> 3
	var x uint64
	if int(i)+8 <= len(r.fb) {
		x = binary.BigEndian.Uint64(r.fb[i:])
	} else {
		for k := 0; k < 8; k++ {
			x <<= 8
			if int(i)+k < len(r.fb) {
				x |= uint64(r.fb[int(i)+k])
			}
		}
	}
	return x << (r.bp & 7)
}

func (r *flacReader) signed(n int) (int64, error) {
	if n == 0 {
		return 0, nil
	}
	v, err := r.bits(uint(n))
	if err != nil {
		return 0, err
	}
	return int64(v<<(64-uint(n))) >> (64 - uint(n)), nil
}

// unary counts zeros up to a one.
func (r *flacReader) unary() (uint64, error) {
	var q uint64
	for {
		if r.bp >= uint(len(r.fb))*8 {
			return 0, io.ErrUnexpectedEOF
		}
		x := r.load()
		valid := 64 - (r.bp & 7) // the bits load gave for sure
		if z := uint(bits.LeadingZeros64(x)); z < valid {
			r.bp += z + 1
			return q + uint64(z), nil
		}
		r.bp += valid
		q += uint64(valid)
	}
}

// skipUTF8 skips the frame number.
func (r *flacReader) skipUTF8() error {
	b, err := r.bits(8)
	if err != nil {
		return err
	}
	extra := 0
	for m := uint64(0x80); b&m != 0 && extra < 7; m >>= 1 {
		extra++
	}
	if extra > 0 {
		extra--
	}
	for range extra {
		if _, err := r.bits(8); err != nil {
			return err
		}
	}
	return nil
}
