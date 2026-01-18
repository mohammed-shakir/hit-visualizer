# hit-visualizer

A visualizer for h3-spatial-cache

## Runtime configuration

The server is configured via environment variables:

- `HTTP_ADDR`: HTTP listen address (default `:8081`).
- `KAFKA_BROKERS`: comma-separated list of Kafka brokers, e.g. `kafka:9092`.
- `KAFKA_TOPIC`: Kafka topic to consume hit events from (default
  `spatial-hit-events`).
- `LOG_LEVEL`: log level (`debug`, `info`, `warn`, `error`), default `info`.

Note: Make sure that your browser's graphics acceleration is on (WebGL enabled)

## Running locally

```bash
set -o allexport; . .env; set +o allexport
go run ./cmd/server
```

Open the UI in your browser:

- [http://localhost:8081/](http://localhost:8081/): serves `frontend/index.html`.
- WebSocket endpoint: `ws://localhost:8081/ws`.

The frontend JS will connect to `/ws`, receive a stream of JSON hit events, and
draw them as pulses on a map.

## Running with Docker Compose

```bash
docker-compose up --build
```

Open the UI:

- [http://localhost:8081/](http://localhost:8081/): serves `frontend/index.html`.
- WebSocket endpoint: `ws://localhost:8081/ws`
