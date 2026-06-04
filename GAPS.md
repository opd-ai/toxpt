# Implementation Gaps — 2026-06-04

This document provides detailed analysis and implementation roadmap for each gap identified in the audit.

---

## Gap 1: Bridge Connection Handler is a No-Op

- **Intended Behavior**: According to the README ("embeddable bridge relay") and PR #1 ("bridge relay lifecycle management"), `handleConn` should relay Tor traffic between the Tox-connected client and the local Tor OR port. The bridge is meant to function as a Tor bridge, forwarding traffic from authenticated Tox friends to the Tor network.

- **Current State**: The `handleConn` method in `bridge.go:85-92` discards all received data:
  ```go
  func (b *EmbeddableBridge) handleConn(ctx context.Context, conn net.Conn) {
      defer b.wg.Done()
      defer conn.Close()
      _, span := b.tracer.Start(ctx, "toxpt.bridge.handle_conn")
      defer span.End()

      _, _ = io.Copy(io.Discard, conn)  // ALL DATA IS DISCARDED
  }
  ```
  This is a stub implementation. The connection is accepted, traced, but all data is thrown away.

- **Blocked Goal**: The project cannot function as a Tor bridge. The stated purpose — "allow Tox users to offer Tor bridges to their friends" — is completely unimplemented.

- **Implementation Path**:
  1. In `handleConn`, dial the local Tor OR port (`b.cfg.BridgeORPort`)
  2. Create a bidirectional relay between `conn` (the Tox-framed connection) and the Tor OR connection
  3. Use `io.Copy` in both directions (two goroutines or `io.CopyBuffer`)
  4. Handle context cancellation for graceful shutdown
  5. Add error handling and logging for relay failures
  6. Record metrics for bytes relayed

  Example implementation outline:
  ```go
  func (b *EmbeddableBridge) handleConn(ctx context.Context, conn net.Conn) {
      defer b.wg.Done()
      defer conn.Close()
      _, span := b.tracer.Start(ctx, "toxpt.bridge.handle_conn")
      defer span.End()

      orAddr := fmt.Sprintf("127.0.0.1:%d", b.cfg.BridgeORPort)
      orConn, err := net.DialContext(ctx, "tcp", orAddr)
      if err != nil {
          b.cfg.Logger.Error("failed to connect to OR port", "error", err)
          return
      }
      defer orConn.Close()

      var wg sync.WaitGroup
      wg.Add(2)

      // Client -> Tor
      go func() {
          defer wg.Done()
          io.Copy(orConn, conn)
      }()

      // Tor -> Client
      go func() {
          defer wg.Done()
          io.Copy(conn, orConn)
      }()

      wg.Wait()
  }
  ```

- **Dependencies**: None. This is a leaf implementation.

- **Effort**: Medium — requires bidirectional relay logic, error handling, graceful shutdown, and additional tests.

---

## Gap 2: ErrUnauthorized Sentinel Never Used

- **Intended Behavior**: Per PR #1 ("Domain errors using go-tor's pkg/errors conventions"), domain-specific errors should be used for categorized error handling. `ErrUnauthorized` was defined for protocol-level authorization failures.

- **Current State**: `errors.go:11` defines:
  ```go
  ErrUnauthorized = torerrors.New(torerrors.CategoryProtocol, torerrors.SeverityHigh, "unauthorized tox peer")
  ```
  However, this error is never returned or wrapped anywhere. When the ACL rejects a connection in `listener.go:41-51`, the connection is simply closed and logged — no error is propagated.

- **Blocked Goal**: Error categorization for unauthorized access is incomplete. Callers cannot programmatically distinguish authorization failures from other errors.

- **Implementation Path**:
  Option A (Recommended): Use the error when rejecting unauthorized connections:
  ```go
  // In listener.go Accept(), after ACL check fails:
  if l.acl != nil && !l.acl.IsAuthorized(req.remoteKey) {
      if req.conn != nil {
          _ = req.conn.Close()
      }
      // Log as before...
      // Return error for metrics/observability:
      return nil, wrapProtocol("unauthorized connection", ErrUnauthorized)
  }
  ```
  
  Option B: Remove the unused sentinel if it's not needed:
  ```go
  // Delete from errors.go:11
  ```

- **Dependencies**: None.

- **Effort**: Small — one-line addition or removal.

---

## Gap 3: ClientPublicKey Field Undocumented and Unvalidated

- **Intended Behavior**: The `Config` struct should have validation for all fields used in the transport, with clear documentation of each field's purpose per PR #1.

- **Current State**: `config.go:24` declares `ClientPublicKey [32]byte` with no documentation. The field is used in `dial.go:11`:
  ```go
  clientKey := t.cfg.ClientPublicKey
  ```
  This key is used as the `remoteKey` in inbound requests to the listener's ACL check. However:
  - No documentation explains what this field represents
  - `Validate()` does not check if it's set
  - The zero value (`[32]byte{}`) would be used silently

- **Blocked Goal**: Configuration validation is incomplete. A user could create a transport with an unconfigured `ClientPublicKey`, leading to confusing ACL behavior.

- **Implementation Path**:
  1. Add documentation to the field:
     ```go
     // ClientPublicKey is the Tox public key used to identify this client
     // when dialing through the transport. Required for client-mode operation.
     ClientPublicKey [32]byte
     ```
  
  2. Add validation in `Validate()` for client-mode scenarios, or document that it can be zero when only server-mode is used:
     ```go
     // Option: Validate if client mode is expected
     if cfg.ClientPublicKey == ([32]byte{}) {
         // Either error or document this is OK for server-only mode
     }
     ```

- **Dependencies**: None.

- **Effort**: Small — documentation and optional validation addition.

---

## Implementation Priority Order

Based on the tiebreaker (stubs on critical paths first):

1. **Gap 1: handleConn stub** (CRITICAL) — Core bridge functionality is missing
2. **Gap 3: ClientPublicKey** (HIGH) — Configuration clarity prevents user errors
3. **Gap 2: ErrUnauthorized** (HIGH) — Error semantics completeness

---

## Verification Checklist

After implementing fixes:

- [ ] `go build ./...` passes
- [ ] `go test -race ./...` passes
- [ ] Test coverage remains ≥80%
- [ ] `go vet ./...` produces no warnings
- [ ] New functionality has corresponding tests
- [ ] Documentation reflects implemented behavior
