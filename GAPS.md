# Implementation Gaps — 2026-06-04

## Gap 1: Bridge Does Not Forward Data to Tor

- **Stated Goal**: The README states toxpt is "a pure-Go Tor pluggable transport that wraps Tor cell payloads in framed Tox messages." A pluggable transport's primary function is to forward traffic between Tor clients and the Tor network.

- **Current State**: The `handleConn` method in `bridge.go:91` reads all data from accepted connections and discards it with `io.Copy(io.Discard, conn)`. No actual Tor relay connection is established, and no data is forwarded.

- **Impact**: The bridge is non-functional as a Tor pluggable transport. Users embedding this library would have a "bridge" that accepts friend connections but does nothing with the traffic, defeating the entire purpose.

- **Closing the Gap**: Implement connection forwarding to connect accepted Tox connections to a Tor OR (Onion Router) port. This typically involves:
  1. Accepting the inbound Tox connection (current code does this)
  2. Dialing the configured `BridgeORPort` on localhost
  3. Copying data bidirectionally between the Tox connection and the Tor OR connection
  4. Handling connection teardown gracefully

## Gap 2: No Outbound Bridge Client Mode

- **Stated Goal**: The README examples show usage from a bridge operator's perspective (server mode). However, pluggable transports typically support both server (bridge) and client modes. A client would need to connect through a friend's bridge.

- **Current State**: The `Dial` method exists and creates connections, but:
  - It only works when a local listener is active (connects to self)
  - It uses `net.Pipe()` for loopback rather than actual Tox network communication
  - There's no mechanism to dial a remote Tox friend's bridge

- **Impact**: Users cannot use toxpt as a Tor client connecting through a friend's bridge. The library is half of a pluggable transport.

- **Closing the Gap**: 
  1. Implement `Dial` to actually send framed data through the Tox client to a specified friend's public key
  2. Register appropriate Tox message handlers for incoming framed data
  3. Expose client-mode configuration for specifying which friend's bridge to use

## Gap 3: Security Configuration Not Actually Enforced

- **Stated Goal**: README claims "Noise-IK encryption", "Forward secrecy", and "All connections use Noise-IK authenticated encryption" as security features.

- **Current State**: The toxpt code itself doesn't implement or verify any encryption. It entirely delegates to the `toxcore.Tox` client passed in by the user. The code:
  - Never calls `GetSecurityPosture()` to verify the claimed security level
  - Has no validation that TCP is enabled (required for forward secrecy per README)
  - Has no validation that UDP is disabled (recommended per README)

- **Impact**: Users may have a false sense of security. If they pass in a misconfigured Tox client (e.g., UDP-only, legacy encryption), the bridge would still function but without the claimed security properties.

- **Closing the Gap**:
  1. Add optional validation in `NewTransport` or `NewEmbeddableBridge` to verify Tox client security posture
  2. Log warnings if security configuration doesn't match claims
  3. Consider making security validation configurable (strict vs permissive)

## Gap 4: Dynamic Friend List Updates Not Propagated

- **Stated Goal**: README states "Friend list updates are reflected automatically when using dynamic mode" and "the bridge dynamically allows all friends from the Tox client."

- **Current State**: The ACL is created once at `NewTransport()` time (or refreshed once at `Start()` time). If friends are added or removed from the Tox client after the transport starts, the ACL is not updated. The code:
  - Creates ACL snapshot at construction: `NewFriendACLFromTox(cfg.ToxClient)`
  - Refreshes once at Start: `t.acl = NewFriendACLFromTox(t.tox)`
  - Never updates afterward

- **Impact**: Adding a new friend while the bridge is running won't allow that friend to use the bridge until restart. Removing a friend won't revoke their access until restart.

- **Closing the Gap**:
  1. Register a callback on the Tox client for friend add/remove events
  2. Update the ACL atomically when friends change
  3. Consider periodic refresh as a fallback

## Gap 5: Connection Handling is Placeholder

- **Stated Goal**: The code structure suggests a complete bridge implementation with connection acceptance, ACL checks, and connection handling.

- **Current State**: Beyond accepting connections and checking ACLs, all connection handling is placeholder:
  - `handleConn` discards all data
  - No bidirectional data flow
  - No connection pooling or rate limiting
  - No metrics on actual data transferred (only connection counts)

- **Impact**: While the structural code is in place, any meaningful bridge operation is missing. The library demonstrates architecture but not function.

- **Closing the Gap**: Implement the connection handling layer to actually process Tor cell data over the Tox transport.

## Gap 6: Missing Error Handling Documentation

- **Stated Goal**: The code exports three sentinel errors (`ErrInvalidConfig`, `ErrUnauthorized`, `ErrNotRunning`) suggesting error handling is a considered API concern.

- **Current State**: While errors exist, there's no documentation on:
  - When each error is returned
  - How callers should handle specific errors
  - Whether errors can be retried
  - Error wrapping conventions used

- **Impact**: Callers cannot reliably distinguish between error types or implement proper error handling.

- **Closing the Gap**: Add GoDoc documentation to each error explaining its meaning and typical causes. Document the wrapping pattern in package-level documentation.
