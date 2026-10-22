// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package netrom

import (
	"sort"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// Defaults from NET/ROM 1.3. Path quality 192 is the published HDLC default
// for a 1200-baud channel. AX/UDP has no baud of its own, so a listed
// neighbor uses that value. Worst quality 0 would ignore every broadcast.
const (
	DefaultPathQuality    = 192
	DefaultMinQuality     = 1
	DefaultObsolescence   = 6
	DefaultMinBroadcast   = 5
	DefaultMaxDestination = 50
	DefaultTTL            = 64
	routesKept            = 3
)

// Combine is the published route quality. Each quality is a fraction of 256,
// rounded to the nearest integer: (broadcast*path + 128) / 256.
func Combine(broadcast, path int) int {
	if broadcast < 0 {
		broadcast = 0
	}
	if path < 0 {
		path = 0
	}
	return (broadcast*path + 128) / 256
}

type route struct {
	via     ax25.Address
	quality int
	obs     int
	locked  bool
}

type destination struct {
	addr   ax25.Address
	alias  [6]byte
	routes []route
}

func (r *Router) SetNeighbors(list []ax25.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := make(map[string]ax25.Address, len(list))
	for _, addr := range list {
		if addr.Call == "" || addr.Equal(r.local) || addr.Call == "NODES" {
			continue
		}
		want[addr.String()] = addr
	}
	for key := range r.neighbors {
		if _, ok := want[key]; ok {
			continue
		}
		delete(r.neighbors, key)
		r.dropViaLocked(key)
	}
	for key, addr := range want {
		if _, ok := r.neighbors[key]; ok {
			continue
		}
		r.neighbors[key] = addr
		r.upsertLocked(addr, Alias(addr.Call), addr, r.pathQuality, true)
	}
}

// Observe applies one routing broadcast from a listed neighbor. Frames from
// any other station are ignored, so a guest cannot publish a next hop.
func (r *Router) Observe(from ax25.Address, info []byte) {
	if r.minQuality < 1 {
		return
	}
	got, ok := DecodeBroadcast(info)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, known := r.neighbors[from.String()]; !known {
		return
	}
	// A direct route to the sender uses the neighbor's path quality.
	r.upsertLocked(from, got.Alias, from, r.pathQuality, true)
	for _, rec := range got.Dests {
		if rec.Call.Equal(r.local) || rec.Call.Equal(from) || rec.Call.Call == "" || rec.Call.Call == "NODES" {
			continue
		}
		// The neighbor named in the record is our own call when the route
		// would come straight back. Those are not installed.
		if rec.Neighbor.Equal(r.local) {
			continue
		}
		quality := Combine(int(rec.Quality), r.pathQuality)
		if quality < r.minQuality || quality > 255 {
			continue
		}
		r.upsertLocked(rec.Call, rec.Alias, from, quality, false)
	}
}

func (r *Router) upsertLocked(dest ax25.Address, alias [6]byte, via ax25.Address, quality int, locked bool) {
	if _, ok := r.neighbors[via.String()]; !ok {
		return
	}
	if quality < r.minQuality {
		return
	}
	key := dest.String()
	d := r.dests[key]
	if d == nil {
		if len(r.dests) >= r.maxDest {
			return
		}
		d = &destination{addr: dest, alias: alias}
		r.dests[key] = d
	} else if aliasText(alias) != "" {
		d.alias = alias
	}
	for i := range d.routes {
		if !d.routes[i].via.Equal(via) {
			continue
		}
		if d.routes[i].locked && !locked {
			d.routes[i].obs = r.obsInit
			return
		}
		d.routes[i].quality = quality
		d.routes[i].obs = r.obsInit
		d.routes[i].locked = d.routes[i].locked || locked
		sortRoutes(d)
		return
	}
	next := route{via: via, quality: quality, obs: r.obsInit, locked: locked}
	if len(d.routes) >= routesKept {
		worst := 0
		for i := 1; i < len(d.routes); i++ {
			if d.routes[i].quality < d.routes[worst].quality {
				worst = i
			}
		}
		if d.routes[worst].locked || quality <= d.routes[worst].quality {
			return
		}
		d.routes[worst] = next
	} else {
		d.routes = append(d.routes, next)
	}
	sortRoutes(d)
}

func sortRoutes(d *destination) {
	sort.Slice(d.routes, func(i, j int) bool {
		if d.routes[i].quality == d.routes[j].quality {
			return d.routes[i].via.String() < d.routes[j].via.String()
		}
		return d.routes[i].quality > d.routes[j].quality
	})
}

func (r *Router) dropViaLocked(viaKey string) {
	for key, d := range r.dests {
		kept := d.routes[:0]
		for _, rt := range d.routes {
			if rt.via.String() == viaKey {
				continue
			}
			kept = append(kept, rt)
		}
		d.routes = kept
		if len(kept) == 0 {
			delete(r.dests, key)
		}
	}
}

// Age decrements the obsolescence count of learned routes. A count that
// reaches zero drops the route. Configured neighbors stay.
func (r *Router) Age() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.obsInit == 0 {
		return
	}
	for key, d := range r.dests {
		kept := d.routes[:0]
		for _, rt := range d.routes {
			if rt.locked {
				kept = append(kept, rt)
				continue
			}
			if rt.obs > 1 {
				rt.obs--
				kept = append(kept, rt)
				continue
			}
		}
		d.routes = kept
		if len(kept) == 0 {
			delete(r.dests, key)
		}
	}
}

// Lookup returns the next hop and quality for a destination.
func (r *Router) Lookup(dest ax25.Address) (ax25.Address, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bestLocked(dest)
}

func (r *Router) bestLocked(dest ax25.Address) (ax25.Address, int, bool) {
	d := r.dests[dest.String()]
	if d == nil {
		return ax25.Address{}, 0, false
	}
	for _, rt := range d.routes {
		if rt.quality < r.minQuality {
			continue
		}
		if _, ok := r.neighbors[rt.via.String()]; !ok {
			continue
		}
		return rt.via, rt.quality, true
	}
	return ax25.Address{}, 0, false
}

// Payloads is the current routing broadcast, one UI information field each.
func (r *Router) Payloads() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	var recs []Record
	keys := make([]string, 0, len(r.dests))
	for key := range r.dests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		d := r.dests[key]
		if d.addr.Equal(r.local) {
			continue
		}
		var top *route
		for i := range d.routes {
			rt := &d.routes[i]
			if rt.quality < r.minQuality {
				continue
			}
			if !rt.locked && rt.obs < r.minBroadcast {
				continue
			}
			if top == nil || rt.quality > top.quality {
				top = rt
			}
		}
		if top == nil {
			continue
		}
		alias := d.alias
		if aliasText(alias) == "" {
			alias = Alias(d.addr.Call)
		}
		recs = append(recs, Record{
			Call:     d.addr,
			Alias:    alias,
			Neighbor: top.via,
			Quality:  byte(top.quality),
		})
	}
	return EncodeBroadcast(r.alias, recs)
}
