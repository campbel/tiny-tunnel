FROM golang:1.22-alpine3.19 as builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /app/bin/tiny-tunnel

FROM alpine:3.19

COPY --from=builder /app/bin/tiny-tunnel /usr/local/bin/tiny-tunnel

EXPOSE 80
EXPOSE 443

ENTRYPOINT [ "/usr/local/bin/tiny-tunnel" ]
