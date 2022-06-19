package bbc

import (
	"bytes"
	"github.com/SharzyL/bbc/bbc/pb"
	"go.uber.org/zap"
	"sync"
	"time"
)

type peerInfo struct {
	addr string

	lastRecvAdvTime   time.Time // zero value means not advertised yet
	lastRecvAdvHeader *pb.BlockHeader

	lastSucAdvTime     time.Time // zero value means not sending advertised yet
	lastTryAdvTime     time.Time // same
	firstFailedAdvTime time.Time // the first failed advertise since last success
	lastSucAdvHeader   *pb.BlockHeader

	isDead bool // by default a peer is alive
}

type peerMgr struct {
	logger *zap.SugaredLogger
	peers  map[string]*peerInfo
	mtx    *sync.RWMutex
}

func newPeerMgr(logger *zap.SugaredLogger) *peerMgr {
	mtx := &sync.RWMutex{}
	return &peerMgr{
		logger: logger,
		peers:  make(map[string]*peerInfo),
		mtx:    mtx,
	}
}

func (p *peerMgr) addPeer(addr string) {
	p.peers[addr] = &peerInfo{addr: addr}
	p.logger.Infow("add new peer", zap.String("addr", addr))
}

func (p *peerMgr) getNextToAdvertise(header *pb.BlockHeader) *peerInfo {
	var minTryAdvTime time.Time
	var peerToAdvertise *peerInfo

	for _, peer := range p.peers {
		// ignore dead node
		if peer.isDead {
			continue
		}

		// if a peer is not advertised yet, advertise it
		if peer.lastTryAdvTime.IsZero() {
			peerToAdvertise = peer
			break
		}

		// if the peer is advertised recently with same block, ignore it
		if time.Now().Sub(peer.lastTryAdvTime) < advertiseInterval && bytes.Equal(Hash(header), Hash(peer.lastSucAdvHeader)) {
			continue
		}

		// if peer is not lower, ignore it
		if !peer.lastRecvAdvTime.IsZero() && peer.lastSucAdvHeader.Height >= header.Height {
			continue
		}

		// otherwise, consider this peer
		if minTryAdvTime.IsZero() || minTryAdvTime.After(peer.lastTryAdvTime) {
			minTryAdvTime = peer.lastTryAdvTime
			peerToAdvertise = peer
		}
	}

	if peerToAdvertise != nil {
		return peerToAdvertise
	} else {
		return nil
	}
}

func (p *peerMgr) onRecvAdvertise(addr string, header *pb.BlockHeader) *peerInfo {
	peer, found := p.peers[addr]
	if !found {
		peer = &peerInfo{addr: addr}
		p.peers[addr] = peer
	}
	now := time.Now()
	peer.lastRecvAdvTime = now
	peer.lastRecvAdvHeader = header
	peer.isDead = false
	return peer
}

func (p *peerMgr) onStartAdvertise(addr string) {
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	peer.lastTryAdvTime = time.Now()
}

func (p *peerMgr) onFailedAdvertise(addr string) {
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	now := time.Now()
	if peer.lastTryAdvTime.IsZero() || peer.firstFailedAdvTime.IsZero() {
		// if not have sent advertise, or last advertise is success
		peer.firstFailedAdvTime = now
	}
	if now.Sub(peer.firstFailedAdvTime) > peerDeadTimeout && !peer.isDead {
		peer.isDead = true
		p.logger.Warnw("peer is dead", zap.String("addr", addr), zap.Time("firstFailed", peer.firstFailedAdvTime))
	}
}

func (p *peerMgr) onSucceedAdvertise(addr string, sendHeader *pb.BlockHeader, recvHeader *pb.BlockHeader) {
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	now := time.Now()
	peer.lastSucAdvTime = now
	peer.lastSucAdvHeader = sendHeader
	peer.lastRecvAdvHeader = recvHeader
	peer.isDead = false
	peer.firstFailedAdvTime = time.Time{}
}

func (p *peerMgr) goForEachAlivePeer(f func(addr string)) {
	for _, peer := range p.peers {
		if !peer.isDead {
			go f(peer.addr)
		}
	}
}
