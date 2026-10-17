package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
)

// Meters: browsers name the meter groups they want when opening the event
// stream (/api/events?meters=1,2,6:12). While a group is wanted, it is kept
// subscribed on the console and its values are forwarded, as "meters"
// events, to the browsers that asked for it only.

// meterKey is "N" or "N:a[:b]" (/meters/N with its int parameters).
type meterKey string

func parseMeterKeys(s string) []meterKey {
	var keys []meterKey
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ok := true
		for _, f := range strings.Split(part, ":") {
			if _, err := strconv.Atoi(f); err != nil {
				ok = false
			}
		}
		if ok {
			keys = append(keys, meterKey(part))
		}
	}
	return keys
}

func (k meterKey) request() (int, []int32) {
	parts := strings.Split(string(k), ":")
	id, _ := strconv.Atoi(parts[0])
	var params []int32
	for _, p := range parts[1:] {
		v, _ := strconv.Atoi(p)
		params = append(params, int32(v))
	}
	return id, params
}

// meterKeyOf is the key of a received /meters/N message. Groups with
// parameters cannot be told apart in replies, so the parameters of the
// first wanted key of that group are assumed.
func (s *Server) meterKeyOf(id int) []meterKey {
	prefix := fmt.Sprint(id)
	var out []meterKey
	for _, k := range s.hub.wantedMeters() {
		if string(k) == prefix || strings.HasPrefix(string(k), prefix+":") {
			out = append(out, k)
		}
	}
	return out
}

// runMeters keeps the wanted meter groups subscribed on the console.
func (s *Server) runMeters(ctx context.Context, c *console.Client) {
	c.OnMessage(func(m osc.Message) {
		id := console.MeterID(m)
		if id < 0 {
			return
		}
		vals, ok := console.DecodeMeters(m)
		if !ok {
			return
		}
		rounded := make([]float64, len(vals))
		for i, v := range vals {
			rounded[i] = math.Round(float64(v)*1e4) / 1e4
		}
		for _, k := range s.meterKeyOf(id) {
			data, _ := json.Marshal(map[string]any{"id": k, "values": rounded})
			s.hub.publishMeters(k, data)
		}
	})
	renew := time.NewTicker(8 * time.Second)
	defer renew.Stop()
	subscribed := map[meterKey]bool{}
	for {
		for _, k := range s.hub.wantedMeters() {
			if !subscribed[k] {
				id, params := k.request()
				c.Meters(id, params...)
				subscribed[k] = true
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.hub.meterKick:
			subscribed = map[meterKey]bool{} // new browser or reconnected console
		case <-renew.C:
			subscribed = map[meterKey]bool{} // renew all on the next round
		}
	}
}
