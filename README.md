# Spanner-Lite: Sharded Distributed Key-Value Store 🌐

A sharded, linearizable key/value storage system built from scratch in Go, heavily inspired by the architecture of Google Spanner and BigTable.

## System Architecture
- **Consensus Layer:** Custom implementation of the **Raft consensus algorithm** .
- **State Machine:** Replicated state machine layer ensuring strong consistency (linearizability) across node crashes and network partitions.
- **Sharding & Scalability:** A dynamic Sharded KV service managed by a high-availability Shard Controller.
- **Dynamic Migration:** Seamless, zero-downtime shard migration across replica groups to balance load and manage cluster configurations.

## Technical Implementation
- Transitioned the academic  MIT 6.5840 framework into a production-ready environment using **gRPC** and **Protocol Buffers**.
- Verified correctness using rigorous concurrency testing, Race Detector, and simulated network faults.