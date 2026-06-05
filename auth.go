package toxpt

import (
	"crypto/subtle"
	"sync"

	"github.com/opd-ai/toxcore"
)

// FriendSource provides a dynamic source of Tox friends.
type FriendSource interface {
	GetFriends() map[uint32]*toxcore.Friend
}

// FriendACL controls which Tox public keys can use the bridge.
type FriendACL struct {
	mu      sync.RWMutex
	friends [][32]byte
}

// NewFriendACL creates an ACL from explicit allowed friend keys.
func NewFriendACL(allowed [][32]byte) *FriendACL {
	copyKeys := make([][32]byte, len(allowed))
	copy(copyKeys, allowed)
	return &FriendACL{friends: copyKeys}
}

// NewFriendACLFromTox creates an ACL from the current tox friend list snapshot.
func NewFriendACLFromTox(t *toxcore.Tox) *FriendACL {
	if t == nil {
		return &FriendACL{}
	}
	return NewFriendACLFromSource(t)
}

// NewFriendACLFromSource creates an ACL from any FriendSource implementation.
func NewFriendACLFromSource(source FriendSource) *FriendACL {
	if source == nil {
		return &FriendACL{}
	}
	friends := source.GetFriends()
	allowed := make([][32]byte, 0, len(friends))
	for _, friend := range friends {
		if friend == nil {
			continue
		}
		allowed = append(allowed, friend.PublicKey)
	}
	return NewFriendACL(allowed)
}

// IsAuthorized checks authorization with constant-time key comparison.
func (a *FriendACL) IsAuthorized(toxPublicKey [32]byte) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	matched := 0
	for _, friendKey := range a.friends {
		matched |= subtle.ConstantTimeCompare(friendKey[:], toxPublicKey[:])
	}
	return matched == 1
}
