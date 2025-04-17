# Tiny Tunnel

Tiny Tunnel is a lightweight, secure solution for exposing local servers to the internet. Perfect for development, demos, and testing webhooks.

## Features

- **Simple Setup**: Get started in minutes with a straightforward CLI
- **Secure**: Built-in authentication and encrypted connections
- **Versatile**: Support for HTTP, WebSockets, and Server-Sent Events
- **Low Overhead**: Minimal resource usage for optimal performance
- **Cross-Platform**: Works on macOS, Linux, and Windows

## Installation

### Using Go

```bash
go install github.com/campbel/tiny-tunnel@latest
```

To install go see https://go.dev/doc/install

### Using Docker

```bash
docker run -it --rm ghcr.io/campbel/tiny-tunnel:latest --help
```

## Quick Start

### Server Setup

To start a tunnel server:

```bash
tiny-tunnel serve
```

By default, this starts the server on port 8080. You can specify a different port:

```bash
tiny-tunnel serve --port 9000
```

### Client Authentication

Before creating tunnels, you need to authenticate with the server:

```bash
tiny-tunnel login localhost:8080
```

This will open a browser window where you can generate and copy an authentication token. The token will be stored in your configuration.

### Creating a Tunnel

To expose a local service:

```bash
tiny-tunnel start --name myapp --target http://localhost:3000
```

This creates a tunnel named "myapp" that forwards requests to your local service running on port 3000.

### Accessing Your Service

Once your tunnel is running, you can access your service at:

```
http://myapp.localhost:8080
```

If you're using a remote server, the URL would use that server's domain.

## Advanced Usage

### Configuration Management

List your saved configurations:

```bash
tiny-tunnel config list
```

Set a default server:

```bash
tiny-tunnel config set-default myserver.example.com
```

### Custom Echo Service

For testing, you can use the built-in echo service:

```bash
tiny-tunnel echo
```

This starts a simple server that echoes back requests, useful for testing your tunnel setup.

## Example Use Cases

- Exposing local development servers for testing
- Demonstrating applications to clients or team members
- Testing webhook integrations
- Quick sharing of local files or services

## Contributing

Contributions are welcome! Feel free to open issues or pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
