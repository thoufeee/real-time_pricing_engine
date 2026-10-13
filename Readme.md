# Real-Time Pricing Engine

A Go-based real-time market pricing service that consumes live market data, calculates rolling 24-hour price statistics, persists market snapshots, and streams updates to subscribed WebSocket clients.

## Prerequisites

- Docker Desktop with the Docker engine running
- Docker Compose
- Market feed API credentials

## Configuration

Create a local `.env` file using the environment variables expected by the application. Set your market feed credentials locally.

Never commit `.env` or real credentials. If credentials were previously exposed, rotate them.

## Run the Application

From the project root:

```bash
docker-compose up --build
```

To run in the background:

```bash
docker-compose up --build -d
```

Check the containers:

```bash
docker-compose ps
```

View application logs:

```bash
docker-compose logs -f
```

Check the health endpoint:

```bash
curl http://localhost:8080/health
```

## WebSocket

Connect using a WebSocket client to:

```text
ws://localhost:8080/ws
```

Subscribe to the symbols you want to receive using the subscription message supported by the application.

## Run Tests

With Go installed locally:

```bash
go test ./...
```

## Documentation

- [Architecture and Data Flow](docs/ARCHITECTURE.md)
- [Challenges Faced and Solutions](docs/CHALLENGES.md)

## Performance Notes

The current payload-size test measured a serialized price snapshot at 154 bytes, below the 10 KB target. End-to-end latency under load has not yet been benchmarked and should be measured before claiming compliance with the latency target.
