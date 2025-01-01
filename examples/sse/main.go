package main

import (
	"fmt"
	"net/http"
	"time"
)

const html = `
<html>
<body>
<h1>Hello, world!</h1>
<div id="count">0</div>
<script>
const eventSource = new EventSource("/events");
eventSource.onmessage = (event) => {
  document.getElementById("count").innerHTML = event.data;
};
</script>
</body>
</html>
`

type Event struct {
	ID   string
	Type string
	Data string
}

func writeEvent(w http.ResponseWriter, event Event) {
	// Write event fields according to SSE specification
	if event.ID != "" {
		fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	if event.Type != "" {
		fmt.Fprintf(w, "event: %s\n", event.Type)
	}
	fmt.Fprintf(w, "data: %s\n\n", event.Data)

	// Flush the response writer
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func main() {
	fmt.Println("Starting server on port 8080")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
	})

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		// prepare the header
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// trap the request under loop forever
		count := 0
		for {
			select {

			// connection is closed then defer will be executed
			case <-r.Context().Done():
				fmt.Println("client disconnected")
				return
			default:
				writeEvent(w, Event{
					ID:   fmt.Sprintf("%d", count),
					Type: "message",
					Data: fmt.Sprintf("%d", count),
				})
				count++
				time.Sleep(1 * time.Second)
			}
		}
	})

	http.ListenAndServe(":8080", nil)
}
