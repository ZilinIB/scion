// Copyright 2017 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package base contains the tables for remote SIGs, ASes and their prefixes
package base

import (
	"sync"

	log "github.com/inconshreveable/log15"

	"github.com/netsec-ethz/scion/go/lib/addr"
	"github.com/netsec-ethz/scion/go/lib/common"
	"github.com/netsec-ethz/scion/go/sig/config"
)

var Map = newASMap()

// ASMap is not concurrency safe against multiple writers.
type ASMap sync.Map

func newASMap() *ASMap {
	return &ASMap{}
}

func (am *ASMap) Delete(key addr.IAInt) {
	(*sync.Map)(am).Delete(key)
}

func (am *ASMap) Load(key addr.IAInt) (*ASEntry, bool) {
	value, ok := (*sync.Map)(am).Load(key)
	if value == nil {
		return nil, ok
	}
	return value.(*ASEntry), ok
}

func (am *ASMap) LoadOrStore(key addr.IAInt, value *ASEntry) (*ASEntry, bool) {
	actual, ok := (*sync.Map)(am).LoadOrStore(key, value)
	if actual == nil {
		return nil, ok
	}
	return actual.(*ASEntry), ok
}

func (am *ASMap) Store(key addr.IAInt, value *ASEntry) {
	(*sync.Map)(am).Store(key, value)
}

func (am *ASMap) Range(f func(key addr.IAInt, value *ASEntry) bool) {
	(*sync.Map)(am).Range(func(key, value interface{}) bool {
		return f(key.(addr.IAInt), value.(*ASEntry))
	})
}

func (am *ASMap) ReloadConfig(cfg *config.Cfg) bool {
	// Method calls first to prevent skips due to logical short-circuit
	s := am.addNewIAs(cfg)
	return am.delOldIAs(cfg) && s
}

// addNewIAs adds the ASes in cfg that are not currently configured.
func (am *ASMap) addNewIAs(cfg *config.Cfg) bool {
	s := true
	for iaVal, cfgEntry := range cfg.ASes {
		ia := &iaVal
		log.Info("ReloadConfig: Adding AS...", "ia", ia)
		ae, err := am.AddIA(ia)
		if err != nil {
			cerr := err.(*common.CError)
			log.Error(cerr.Desc, cerr.Ctx...)
			s = false
			continue
		}
		s = ae.ReloadConfig(cfgEntry) && s
		log.Info("ReloadConfig: Added AS", "ia", ia)
	}
	return s
}

func (am *ASMap) delOldIAs(cfg *config.Cfg) bool {
	s := true
	// Delete all ASes that currently exist but are not in cfg
	am.Range(func(iaInt addr.IAInt, as *ASEntry) bool {
		ia := iaInt.IA()
		if _, ok := cfg.ASes[*ia]; !ok {
			log.Info("ReloadConfig: Deleting AS...", "ia", ia)
			// Deletion also handles session/tun device cleanup
			err := am.DelIA(ia)
			if err != nil {
				cerr := err.(*common.CError)
				log.Error(cerr.Desc, cerr.Ctx...)
				s = false
				return true
			}
			log.Info("ReloadConfig: Deleted AS", "ia", ia)
		}
		return true
	})
	return s
}

// AddIA idempotently adds an entry for a remote IA.
func (am *ASMap) AddIA(ia *addr.ISD_AS) (*ASEntry, error) {
	if ia.I == 0 || ia.A == 0 {
		// A 0 for either ISD or AS indicates a wildcard, and not a specific ISD-AS.
		return nil, common.NewCError("AddIA: ISD and AS must not be 0", "ia", ia)
	}
	key := ia.IAInt()
	ae, ok := am.Load(key)
	if ok {
		return ae, nil
	}
	ae, err := newASEntry(ia)
	if err != nil {
		return nil, err
	}
	am.Store(key, ae)
	return ae, nil
}

// DelIA removes an entry for a remote IA.
func (am *ASMap) DelIA(ia *addr.ISD_AS) error {
	key := ia.IAInt()
	ae, ok := am.Load(key)
	if !ok {
		return common.NewCError("DelIA: No entry found", "ia", ia)
	}
	am.Delete(key)
	return ae.Cleanup()
}

// ASEntry returns the entry for the specified remote IA, or nil if not present.
func (am *ASMap) ASEntry(ia *addr.ISD_AS) *ASEntry {
	if as, ok := am.Load(ia.IAInt()); ok {
		return as
	}
	return nil
}
