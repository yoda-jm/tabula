package console

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Meters asks the console for meter group id (/meters/<id>, see the
// protocol document) for 10 s; call it again (or Renew) before that to keep
// receiving. Values arrive as messages to OnMessage handlers; DecodeMeters
// reads them.
func (c *Client) Meters(id int, params ...int32) error {
	args := []osc.Arg{osc.String(fmt.Sprintf("/meters/%d", id))}
	for _, p := range params {
		args = append(args, osc.Int(p))
	}
	return c.send(osc.NewMessage("/meters", args...))
}

// MeterID returns the meter group of a /meters/N message, or -1.
func MeterID(m osc.Message) int {
	var id int
	if _, err := fmt.Sscanf(m.Address, "/meters/%d", &id); err != nil || !strings.HasPrefix(m.Address, "/meters/") {
		return -1
	}
	return id
}

// DecodeMeters returns the values of a /meters/N message: linear levels
// (1.0 = 0 dBFS) for most groups, dB values for the RTA (group 15) and
// gains in [0, 1] for group 16 (gate, compressor and automix gains).
func DecodeMeters(m osc.Message) ([]float32, bool) {
	id := MeterID(m)
	if id < 0 || len(m.Args) != 1 || m.Args[0].Tag != 'b' || len(m.Args[0].Blob) < 4 {
		return nil, false
	}
	b := m.Args[0].Blob
	n := int(binary.LittleEndian.Uint32(b))
	if n < 0 || 4+4*n > len(b) {
		return nil, false
	}
	words := make([]uint32, n)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(b[4+4*i:])
	}
	switch id {
	case 15: // 100 RTA bands, dB × 256 as int16
		out := make([]float32, 0, 2*n)
		for _, w := range words {
			out = append(out, float32(int16(w))/256, float32(int16(w>>16))/256)
		}
		return out, true
	case 16: // gains as int16/32767, two per word
		out := make([]float32, 0, 2*n)
		for _, w := range words {
			out = append(out, float32(int16(w))/32767, float32(int16(w>>16))/32767)
		}
		return out, true
	}
	out := make([]float32, n)
	for i, w := range words {
		out[i] = math.Float32frombits(w)
	}
	return out, true
}
