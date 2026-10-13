
# Challenges Faced and Solutions

This document records the main engineering challenges encountered while developing the Real-Time Pricing Engine, the solutions implemented, and the remaining areas that need validation.

## 1. Docker Environment and Startup

**Challenge:** Docker Compose initially failed to connect to the Docker Desktop Linux engine.

**Solution:** Checked the Docker Desktop engine and container startup, then rebuilt and ran the services after Docker became available.

**Learning:** A valid Compose configuration still requires the Docker engine to be running.

**Further improvement:** Add a clear startup checklist for Docker availability, container health, and service connectivity.

## 2. Provider Payload Compatibility

**Challenge:** The market feed uses provider-specific fields such as `bid`, `ask`, and `percentage`. Some messages may not contain a timestamp.

**Solution:** Added payload decoding to map provider fields into the application's market tick model and handle missing timestamps.

**Trade-off:** Using local receive time when a provider timestamp is missing allows processing to continue, but it is not equivalent to the original event time. This distinction matters for ordering and rolling statistics.

**Further improvement:** Track whether a timestamp came from the provider or was generated locally, and test mixed timestamp formats.

## 3. WebSocket Connection Handling

**Challenge:** WebSocket connections can fail when multiple goroutines perform conflicting reads or writes on the same connection.

**Solution:** Updated connection lifecycle handling and separated incoming-message reading from outgoing-message writing while supporting subscriptions.

**Learning:** WebSocket concurrency must be managed carefully to avoid connection errors and inconsistent message delivery.

**Further improvement:** Test concurrent subscriptions, disconnects, slow clients, and cleanup under high connection counts.

## 4. Duplicate and Out-of-Order Market Data

**Challenge:** Repeated or delayed ticks can distort price history and market statistics if processed without safeguards.

**Solution:** Added duplicate detection and timestamp-aware processing so an older tick does not replace the latest quote.

**Remaining consideration:** Rolling-window expiration and unusual event-time sequences still require thorough testing.

**Further improvement:** Add tests for equal timestamps, delayed ticks, duplicate messages, and large timestamp gaps.

## 5. Test Discovery and Payload Size

**Challenge:** Go does not discover test functions in files that do not follow its test-file naming convention.

**Solution:** Renamed the pricing engine test file to use the `_test.go` suffix and added tests for price calculations, duplicate handling, out-of-order ticks, array decoding, and serialized payload size.

**Observed result:** The serialized `PriceSnapshot` test payload measured 154 bytes, below the 10 KB requirement for that payload.

**Important distinction:** The payload-size result does not establish end-to-end processing latency or WebSocket delivery latency.

## 6. Historical State Restoration

**Challenge:** Reconstructing in-memory pricing state requires loading historical records from persistent storage. Large datasets can increase startup time and memory consumption.

**Solution:** Restored historical price snapshots from PostgreSQL during startup.

**Further improvement:** Benchmark restoration time, evaluate query plans and indexes, and confirm that the restored history covers the required rolling-window period.

## Performance and Trade-offs

- In-memory state supports quick access to current prices but requires persistence and restoration to recover after a restart.
- PostgreSQL provides durable historical storage, with database I/O adding overhead.
- Compact JSON snapshots reduce WebSocket bandwidth.
- Duplicate detection and historical-window maintenance improve correctness but consume memory and processing time.
- Redis is configured as supporting infrastructure; its performance impact depends on the operations actually performed.
- A measured latency benchmark is still needed before claiming that the 50 ms latency requirement is satisfied.

## Further Improvements

- Benchmark price processing and end-to-end WebSocket delivery under realistic load.
- Test application restart and historical state restoration.
- Test provider reconnection, authentication failures, and active-session restrictions.
- Test malformed payloads and high-volume updates.
- Test rolling 24-hour expiration and memory usage under sustained traffic.
- Measure database query latency and Redis operation latency where applicable.
- Test slow WebSocket clients, concurrent connections, and subscription cleanup.
- Add metrics for feed status, tick processing, dropped or duplicate ticks, and delivery latency.
