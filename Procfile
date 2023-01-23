echo:   go run main.go echo -p 7000   | jq -c
server: go run main.go server -p 8000 | jq -c
client: go run main.go client --name dev-tunnel --server-host localhost --server-port 8000 --insecure --header foo=bar --header baz=qux http://127.0.0.1:7000 | jq -c