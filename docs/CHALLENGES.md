# Challenges Faced and Solutions

## 1. Docker Environment and Startup

**Challenge:** Docker Compose initially failed to connect to the Docker Desktop Linux engine.

**Solution:** Checked the Docker Desktop engine and container startup, then rebuilt and ran the services after Docker became available.

**Learning:** A valid Compose configuration still requires the Docker engine to be running.

## 2. Provider Payload Compatibility

**Challenge:** The market feed uses provider-specific field names such as `bid`, `ask`, and `percentage`, and its messages may not include a timestamp.

**Solution:** Added payload decoding to map provider fields into the application's market tick model and handle missing timestamps.

**Trade-off:** Using the local receive time for a message without a provider timestamp allows processing to continue, but it is not equivalent to the provider's original event time.

## 3. WebSocket Connection Handling

**Challenge:** WebSocket connection handling needed to avoid competing reads and writes on a connection.

**Solution:** Updated the connection lifecycle and separated message reading from outgoing message writing, while supporting subscriptions and connection handling.

**Learning:** WebSocket concurrency must be managed carefully to avoid connection errors and inconsistent message delivery.

## 4. Duplicate and Out-of-Order Market Data

**Challenge:** Repeated or delayed ticks can affect price history and market statistics if they are processed without safeguards.

**Solution:** Added duplicate detection and timestamp-aware processing so an older tick does not replace the latest quote.

**Remaining consideration:** Rolling-window expiration and unusual event-time sequences should be tested thoroughly.

## 5. Test Discovery and Payload Size

**Challenge:** Go does not automatically discover test files unless their names follow the expected `_test.go` convention.

**Solution:** Renamed the pricing engine test file and added tests for price calculations, duplicates, out-of-order ticks, array decoding, and serialized payload size.

**Observed result:** The serialized `PriceSnapshot` test payload measured 154 bytes, below the 10 KB requirement for that payload.

## Performance and Trade-offs

- In-memory state supports quick access to current market data but requires persistence and restoration to recover after a restart.
- PostgreSQL provides durable historical storage, with database I/O adding overhead.
- Compact JSON snapshots reduce WebSocket bandwidth.
- Duplicate detection and historical-window maintenance improve correctness but consume memory and processing time.
- A measured latency benchmark is still needed before claiming that the 50 ms latency requirement is satisfied.

## Further Improvements

- Benchmark processing and end-to-end WebSocket delivery under realistic load.
- Test application restart and state restoration.
- Test feed reconnection, malformed messages, and high-volume updates.
- Review rolling 24-hour expiration and memory usage under sustained traffic.
