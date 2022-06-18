package bbc

import (
	"github.com/SharzyL/bbc/bbc/pb"
	"go.uber.org/zap"
	"sync"
	"time"
)

type peerInfo struct {
	addr string

	lastRecvAdvertise       time.Time // zero value means not advertised yet
	lastRecvAdvertiseHeight int64

	lastSucAdvertise       time.Time // zero value means not sending advertised yet
	lastTryAdvertise       time.Time // same
	firstFailedAdvertise   time.Time // the first failed advertise since last success
	lastSucAdvertiseHeight int64

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
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.peers[addr] = &peerInfo{addr: addr}
	p.logger.Infow("add new peer", zap.String("addr", addr))
}

func (p *peerMgr) getNextToAdvertise(header *pb.BlockHeader) *peerInfo {
	var minTryAdvTime time.Time
	var peerToAdvertise *peerInfo

	p.mtx.RLock()
	for _, peer := range p.peers {
		if peer.isDead {
			continue
		}

		if time.Now().Sub(peer.lastTryAdvertise) < advertiseInterval {
			continue
		}

		if !peer.lastRecvAdvertise.IsZero() && peer.lastRecvAdvertiseHeight >= header.Height {
			// if peer is not lower
			continue
		}

		// if a peer is not advertised yet, advertise it
		if peer.lastTryAdvertise.IsZero() {
			peerToAdvertise = peer
			break
		}

		if minTryAdvTime.IsZero() || minTryAdvTime.After(peer.lastTryAdvertise) {
			minTryAdvTime = peer.lastTryAdvertise
			peerToAdvertise = peer
		}
	}
	p.mtx.RUnlock()

	if peerToAdvertise != nil {
		return peerToAdvertise
	} else {
		return nil
	}
}

func (p *peerMgr) onRecvAdvertise(addr string, header *pb.BlockHeader) *peerInfo {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	peer, found := p.peers[addr]
	if !found {
		peer = &peerInfo{addr: addr}
		p.peers[addr] = peer
	}
	now := time.Now()
	peer.lastRecvAdvertise = now
	peer.lastRecvAdvertiseHeight = header.Height
	peer.isDead = false
	return peer
}

func (p *peerMgr) onStartAdvertise(addr string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	peer.lastTryAdvertise = time.Now()
}

func (p *peerMgr) onFailedAdvertise(addr string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	now := time.Now()
	if peer.lastTryAdvertise.IsZero() || peer.firstFailedAdvertise.IsZero() {
		// if not have sent advertise, or last advertise is success
		peer.firstFailedAdvertise = now
	}
	if now.Sub(peer.firstFailedAdvertise) > peerDeadTimeout && !peer.isDead {
		peer.isDead = true
		p.logger.Warnw("peer is dead", zap.String("addr", addr), zap.Time("firstFailed", peer.firstFailedAdvertise))
	}
}

func (p *peerMgr) onSucceedAdvertise(addr string, header *pb.BlockHeader) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	peer := p.peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	now := time.Now()
	peer.lastSucAdvertise = now
	peer.lastRecvAdvertiseHeight = header.Height
	peer.isDead = false
	peer.firstFailedAdvertise = time.Time{}
}

func (p *peerMgr) goForEachAlivePeer(f func(addr string)) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	for _, peer := range p.peers {
		if !peer.isDead {
			go f(peer.addr)
		}
	}
}
