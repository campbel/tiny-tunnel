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

### Client Authentication

Before creating tunnels, you need to authenticate with the server:

```bash
tiny-tunnel login example.com
```

This will open a browser window where you can generate and copy an authentication token. The token will be stored in the configuration at `~/.config/tiny-tunnel/auth.json`

### Creating a Tunnel

To expose a local service:

```bash
tiny-tunnel start --name myapp --target http://localhost:3000
```

This creates a tunnel named "myapp" that forwards requests to your local service running on port 3000.

### Accessing Your Service

Once your tunnel is running, you can access your service like:

```
https://myapp.example.com
```

## Example Use Cases

- Exposing local development servers for testing
- Demonstrating applications to clients or team members
- Testing webhook integrations
- Quick sharing of local files or services

## Contributing

Contributions are welcome! Feel free to open issues or pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
