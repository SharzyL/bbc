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
	lastRecvAdvHeight int64 // this might be taller than lastRecvAdvHeader

	lastSucAdvTime time.Time // zero value means not sending advertised yet

	lastTryAdvTime   time.Time // same
	lastTryAdvHeader *pb.BlockHeader

	firstFailedAdvTime time.Time // the first failed advertise since last success

	isDead bool // by default a peer is alive
}

type peerMgr struct {
	Logger *zap.SugaredLogger

	SelfAddr string
	Peers    map[string]*peerInfo
	Mtx      *sync.RWMutex
}

func newPeerMgr(logger *zap.SugaredLogger, selfAddr string) *peerMgr {
	mtx := &sync.RWMutex{}
	return &peerMgr{
		Logger:   logger,
		SelfAddr: selfAddr,
		Peers:    make(map[string]*peerInfo),
		Mtx:      mtx,
	}
}

func (p *peerMgr) addPeer(addr string) *peerInfo {
	peer := &peerInfo{addr: addr}
	p.Peers[addr] = peer
	p.Logger.Infow("add new peer", zap.String("addr", addr))
	return peer
}

func (p *peerMgr) getNextToAdvertise(header *pb.BlockHeader) *peerInfo {
	var minTryAdvTime time.Time
	var peerToAdvertise *peerInfo

	for _, peer := range p.Peers {
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
		if time.Now().Sub(peer.lastTryAdvTime) < advertiseInterval && bytes.Equal(Hash(header), Hash(peer.lastTryAdvHeader)) {
			continue
		}

		// if peer is not lower, ignore it
		if !peer.lastRecvAdvTime.IsZero() && peer.lastRecvAdvHeight >= header.Height {
			continue
		}

		// otherwise, consider this peer
		if minTryAdvTime.IsZero() || minTryAdvTime.After(peer.lastTryAdvTime) {
			minTryAdvTime = peer.lastTryAdvTime
			peerToAdvertise = peer
		}
	}
	return peerToAdvertise
}

func (p *peerMgr) onRecvAdvertise(req *pb.AdvertiseBlockReq) *peerInfo {
	addr := req.Addr
	peer, found := p.Peers[addr]
	if !found {
		peer = &peerInfo{addr: addr}
		p.Peers[addr] = peer
	}
	now := time.Now()
	peer.lastRecvAdvTime = now
	peer.lastRecvAdvHeader = req.Header
	peer.lastRecvAdvHeight = req.Header.Height
	peer.isDead = false

	for i, peer := range req.Peers {
		if peer == p.SelfAddr {
			continue
		}
		info, found := p.Peers[peer]
		if !found {
			info = p.addPeer(peer)
		}
		// we take max value, since the peer might be inhonest, or not up to date
		info.lastRecvAdvHeight = maxInt64(info.lastRecvAdvHeight, req.Heights[i])
	}
	return peer
}

func (p *peerMgr) onStartAdvertise(addr string, header *pb.BlockHeader) {
	peer := p.Peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	peer.lastTryAdvHeader = header
	peer.lastTryAdvTime = time.Now()
}

func (p *peerMgr) onFailedAdvertise(addr string) {
	peer := p.Peers[addr]
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
		p.Logger.Warnw("peer is dead", zap.String("addr", addr), zap.Time("firstFailed", peer.firstFailedAdvTime))
	}
}

func (p *peerMgr) onSucceedAdvertise(addr string, recvHeader *pb.BlockHeader) {
	peer := p.Peers[addr]
	if peer == nil {
		panic("nil peer")
	}
	now := time.Now()
	peer.lastSucAdvTime = now
	peer.lastRecvAdvHeader = recvHeader
	peer.lastRecvAdvHeight = recvHeader.Height
	peer.isDead = false
	peer.firstFailedAdvTime = time.Time{}
}

func (p *peerMgr) goForEachAlivePeer(f func(addr string)) {
	for _, peer := range p.Peers {
		if !peer.isDead {
			go f(peer.addr)
		}
	}
}
