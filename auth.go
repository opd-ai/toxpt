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
	return newFriendACLFromSource(t)
}

func newFriendACLFromSource(source FriendSource) *FriendACL {
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
	// Always do at least one constant-time comparison to avoid timing leaks.
	// If no friends are configured, we still compare with a dummy zero key to maintain
	// constant time even for empty ACL cases.
	if len(a.friends) == 0 {
		zeroKey := [32]byte{}
		matched = subtle.ConstantTimeCompare(zeroKey[:], toxPublicKey[:])
		// Ensure the dummy comparison never matches (result should be 0)
		matched = 0
	} else {
		for _, friendKey := range a.friends {
			matched |= subtle.ConstantTimeCompare(friendKey[:], toxPublicKey[:])
		}
	}
	return matched == 1
}
