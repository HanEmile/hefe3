package sfu

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/pion/webrtc/v4"
)

// Peer represents a participant in a room.
type Peer struct {
	ID         string
	UserID     string
	Connection *webrtc.PeerConnection
	tracks     []*webrtc.TrackLocalStaticRTP
}

// Room manages peers and track forwarding for a single call.
type Room struct {
	id     string
	mu     sync.RWMutex
	peers  map[string]*Peer
	config webrtc.Configuration
	logger *slog.Logger
}

func newRoom(id string, config webrtc.Configuration, logger *slog.Logger) *Room {
	return &Room{
		id:     id,
		peers:  make(map[string]*Peer),
		config: config,
		logger: logger,
	}
}

// AddPeer creates a new peer connection and adds it to the room.
func (r *Room) AddPeer(peerID, userID string) (*Peer, *webrtc.PeerConnection, error) {
	pc, err := webrtc.NewPeerConnection(r.config)
	if err != nil {
		return nil, nil, fmt.Errorf("create peer connection: %w", err)
	}

	peer := &Peer{
		ID:         peerID,
		UserID:     userID,
		Connection: pc,
	}

	// When this peer sends a track, forward it to all other peers
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.logger.Info("track received",
			"room", r.id,
			"peer", peerID,
			"kind", remoteTrack.Kind(),
			"codec", remoteTrack.Codec().MimeType,
		)

		// Create a local track to forward
		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			remoteTrack.ID(),
			remoteTrack.StreamID(),
		)
		if err != nil {
			r.logger.Error("create local track", "error", err)
			return
		}

		r.mu.Lock()
		peer.tracks = append(peer.tracks, localTrack)
		r.mu.Unlock()

		// Add this track to all other peers
		r.addTrackToOtherPeers(peerID, localTrack)

		// Forward RTP packets
		go func() {
			buf := make([]byte, 1500)
			for {
				n, _, readErr := remoteTrack.Read(buf)
				if readErr != nil {
					r.logger.Debug("track read ended", "peer", peerID, "error", readErr)
					return
				}
				if _, writeErr := localTrack.Write(buf[:n]); writeErr != nil {
					return
				}
			}
		}()
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		r.logger.Info("ice state changed",
			"room", r.id,
			"peer", peerID,
			"state", state.String(),
		)

		if state == webrtc.ICEConnectionStateDisconnected ||
			state == webrtc.ICEConnectionStateFailed ||
			state == webrtc.ICEConnectionStateClosed {
			r.RemovePeer(peerID)
		}
	})

	r.mu.Lock()
	// Add existing tracks from other peers to this new peer
	for otherID, otherPeer := range r.peers {
		if otherID == peerID {
			continue
		}
		for _, track := range otherPeer.tracks {
			if _, err := pc.AddTrack(track); err != nil {
				r.logger.Error("add existing track to new peer",
					"error", err,
					"from", otherID,
					"to", peerID,
				)
			}
		}
	}
	r.peers[peerID] = peer
	r.mu.Unlock()

	return peer, pc, nil
}

// RemovePeer removes a peer and closes its connection.
func (r *Room) RemovePeer(peerID string) {
	r.mu.Lock()
	peer, ok := r.peers[peerID]
	if ok {
		delete(r.peers, peerID)
	}
	r.mu.Unlock()

	if ok && peer.Connection != nil {
		peer.Connection.Close()
	}
}

// PeerCount returns the number of connected peers.
func (r *Room) PeerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}

// PeerIDs returns the IDs of all connected peers.
func (r *Room) PeerIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.peers))
	for id := range r.peers {
		ids = append(ids, id)
	}
	return ids
}

// GetPeer returns a peer by ID.
func (r *Room) GetPeer(peerID string) (*Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[peerID]
	return p, ok
}

func (r *Room) addTrackToOtherPeers(senderID string, track *webrtc.TrackLocalStaticRTP) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, peer := range r.peers {
		if id == senderID {
			continue
		}
		if _, err := peer.Connection.AddTrack(track); err != nil {
			r.logger.Error("add track to peer",
				"error", err,
				"from", senderID,
				"to", id,
			)
		}
	}
}
