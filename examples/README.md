# Tiny Tunnel Example Applications

This directory contains simple Go applications for testing Tiny Tunnel functionality.

## Examples

### 1. REST API (examples/rest-api)

A simple HTTP server that provides various API endpoints using different HTTP methods.

- GET endpoint: `/api/message`
- POST endpoint: `/api/submit`
- PUT endpoint: `/api/update`
- DELETE endpoint: `/api/delete`

To run:
```
go run ./examples/rest-api [port]  # Default port is 8021
```

You can also set the port using the PORT environment variable:
```
PORT=9000 go run ./examples/rest-api
```

### 2. WebSocket Counter (examples/websocket-counter)

A WebSocket server that sends incrementing counter values to connected clients.

- WebSocket endpoint: `/ws`

To run:
```
go run ./examples/websocket-counter [port]  # Default port is 8022
```

You can also set the port using the PORT environment variable:
```
PORT=9000 go run ./examples/websocket-counter
```

### 3. SSE Counter (examples/sse-counter)

A Server-Sent Events (SSE) server that sends incrementing counter values to connected clients.

- SSE endpoint: `/events`

To run:
```
go run ./examples/sse-counter [port]  # Default port is 8023
```

You can also set the port using the PORT environment variable:
```
PORT=9000 go run ./examples/sse-counter
```

## Using with Tiny Tunnel

To tunnel these examples through Tiny Tunnel:

1. Start one of the example servers (e.g., `go run ./examples/rest-api`)
2. In another terminal, start Tiny Tunnel:
   ```
   go run main.go start http://localhost:8021
   ```
   For WebSocket or SSE examples, use the appropriate port:
   ```
   go run main.go start http://localhost:8022  # WebSocket example
   go run main.go start http://localhost:8023  # SSE example
   ```
3. Access the tunneled application through the Tiny Tunnel URL

## Dependencies

The WebSocket example requires the Gorilla WebSocket package:

```
go get github.com/gorilla/websocket
```