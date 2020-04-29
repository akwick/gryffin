// Copyright 2015, Yahoo Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gryffin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	distance "github.com/yahoo/gryffin/html-distance"
)

// GryffinStore includes data and handles for Gryffin message processing,
type GryffinStore struct {
	Oracles map[string]*distance.Oracle
	Hashes  map[string]bool
	Hits    map[string]int
	Mu      sync.RWMutex
	// store   data.Store - currently unused, TODO: use or remove
	snd chan []byte
	rcv chan []byte
}

// PublishMessage is the data in the messages handled by Gryffin.
type PublishMessage struct {
	F string // function, i.e. See or Seen
	T string // type (kind), i.e. oracle or hash
	K string // key
	V string // value
}

func NewSharedGryffinStore() *GryffinStore {
	return newGryffinStore(true)
}

func NewGryffinStore() *GryffinStore {
	return newGryffinStore(false)
}

func newGryffinStore(shared bool) *GryffinStore {

	store := GryffinStore{
		Oracles: make(map[string]*distance.Oracle),
		Hashes:  make(map[string]bool),
		Hits:    make(map[string]int),
	}

	if shared {
		store.snd = make(chan []byte, 10)
		store.rcv = make(chan []byte, 10)
	}

	// start a go rountine to read from the channel
	go store.processRcvMsg()

	return &store
}

func (s *GryffinStore) GetRcvChan() chan []byte {
	return s.rcv
}

func (s *GryffinStore) GetSndChan() chan []byte {
	return s.snd
}

func (s *GryffinStore) processRcvMsg() {
	for jsonPayload := range s.rcv {
		var m PublishMessage
		err := json.Unmarshal(jsonPayload, &m)
		if err != nil {
			fmt.Println("Error in processRcvMsg")
			continue
		}
		fmt.Println("Got a RcvMsg: ", m) // DEBUG
		if m.F == "See" {
			v, _ := strconv.ParseUint(m.V, 16, 64)
			switch m.T {
			case "hash":
				s.hashesSee(m.K, v, true)
			case "oracle":
				s.oracleSee(m.K, v, true)
			}
		}
	}
}

func (s *GryffinStore) See(prefix string, kind string, v uint64) {

	if kind == "oracle" {
		s.oracleSee(prefix, v, false)
		return
	}
	if kind == "hash" {
		s.hashesSee(prefix, v, false)
		return
	}
}

func (s *GryffinStore) Seen(prefix string, kind string, v uint64, r uint8) bool {

	switch kind {
	case "oracle":
		s.Mu.RLock()
		if oracle, ok := s.Oracles[prefix]; ok {
			s.Mu.RUnlock()
			return oracle.Seen(v, r)
		}
		s.Mu.RUnlock()
	case "hash":
		k := prefix + "/" + strconv.FormatUint(v, 10)
		s.Mu.RLock()
		_, ok := s.Hashes[k]
		s.Mu.RUnlock()
		return ok
	}
	return false
}

func (s *GryffinStore) oracleSee(prefix string, f uint64, localOnly bool) {
	k := prefix
	// Local update
	s.Mu.RLock()
	oracle, ok := s.Oracles[k]
	s.Mu.RUnlock()
	if !ok {
		s.Mu.Lock()
		s.Oracles[k] = distance.NewOracle()
		oracle = s.Oracles[k]
		s.Mu.Unlock()
	}
	oracle.See(f)

	// Remote update
	if !localOnly && s.snd != nil {
		go func() {
			jsonPayload, _ := json.Marshal(&PublishMessage{F: "See", T: "oracle", K: prefix, V: fmt.Sprintf("%x", f)})
			// fmt.Println("Sending... ", s.snd, string(jsonPayload))
			s.snd <- jsonPayload
		}()
	}
}

func (s *GryffinStore) hashesSee(prefix string, h uint64, localOnly bool) {
	k := prefix + "/" + strconv.FormatUint(h, 10)
	s.Mu.Lock()
	s.Hashes[k] = true
	s.Mu.Unlock()
	// Remote update
	if !localOnly && s.snd != nil {
		go func() {
			jsonPayload, _ := json.Marshal(&PublishMessage{F: "See", T: "hash", K: prefix, V: fmt.Sprintf("%x", h)})
			s.snd <- jsonPayload
		}()
	}
}

func (s *GryffinStore) Hit(prefix string) bool {
	// prefix is domain.
	ts := time.Now().Truncate(5 * time.Second).Unix()
	k := prefix + "/" + strconv.FormatInt(ts, 10)
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if v, ok := s.Hits[k]; ok {
		if v >= 5 {
			return false
		}
		s.Hits[k]++
		return true
	}
	s.Hits[k] = 1
	return true
}
