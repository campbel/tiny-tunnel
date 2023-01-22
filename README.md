# Tiny Tunnel

It's a tiny HTTP tunnel, what else could it be.

## Docker

No provided docker images, but just install from go alpine.

```bash
docker run \
    -it \
    --rm \
    -p 8000:8000 \
    --env TT_VERSION=latest \
    --env TT_SERVER_PORT=8000 \
    --name tiny-tunnel \
    golang:alpine \
    /bin/sh -c "go install \
    github.com/campbel/tiny-tunnel@\$TT_VERSION && \
    tiny-tunnel server"
```
