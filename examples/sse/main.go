package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"

	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/google/uuid"
)

//go:embed templates
var templates embed.FS

var htmlTemplate = template.Must(template.New("html").Parse(mustReadFile("templates/index.html")))

func mustReadFile(name string) string {
	data, err := templates.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

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

type SSEHandler func(context.Context, chan string)

var dataChans = safe.NewMap[string, chan string]()
var sseHandlers = safe.NewMap[string, SSEHandler]()

func main() {
	fmt.Println("Starting server on port 8080")

	homeTemplate := template.Must(template.New("home").Parse(mustReadFile("templates/home.html")))
	sseHandlers.SetNX("/", func(ctx context.Context, ch chan string) {
		var buf bytes.Buffer
		homeTemplate.Execute(&buf, nil)
		ch <- buf.String()
	})

	fooTemplate := template.Must(template.New("foo").Parse(mustReadFile("templates/foo.html")))
	sseHandlers.SetNX("/foo", func(ctx context.Context, ch chan string) {
		var buf bytes.Buffer
		fooTemplate.Execute(&buf, nil)
		ch <- buf.String()
	})

	handleNewClient := func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		htmlTemplate.Execute(w, map[string]string{
			"ID": id,
		})
		ch := make(chan string)
		dataChans.SetNX(id, ch)
		handler, ok := sseHandlers.Get(r.URL.Path)
		if !ok {
			ch <- `<h2>Not found</h2>`
			return
		}
		go handler(r.Context(), ch)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			handleNewClient(w, r)
		} else {
			ch, ok := dataChans.Get(id)
			if !ok {
				handleNewClient(w, r)
				return
			}
			handler, ok := sseHandlers.Get(r.URL.Path)
			if !ok {
				ch <- `<h2>Not found</h2>`
				return
			}
			go handler(r.Context(), ch)
		}
	})

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		ch, ok := dataChans.Get(id)
		if !ok {
			http.Error(w, "client not found", http.StatusNotFound)
			return
		}
		// prepare the header
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		for {
			select {
			case data := <-ch:
				writeEvent(w, Event{
					ID:   id,
					Type: "message",
					Data: base64.StdEncoding.EncodeToString([]byte(data)),
				})
			// connection is closed then defer will be executed
			case <-r.Context().Done():
				fmt.Println("client disconnected")
				return
			}
		}
	})

	http.ListenAndServe(":8080", nil)
}
