echo: TT_ECHO_PORT=7000 go run main.go echo | jq -c
server: TT_SERVER_PORT=8000 go run main.go server | jq -c
client: go run main.go client --name dev-tunnel --server-host localhost --server-port 8000 localhost --insecure --header foo=bar --header baz=qux http://127.0.0.1:7000 | jq -c