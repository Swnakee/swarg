package mcptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ---------------------------------------------------------------------------
// Streamable HTTP fake
// ---------------------------------------------------------------------------

// HTTPServer is the fake reached by URL: an http.Handler around a Server (serve it with httptest).
type HTTPServer struct {
	*Server
	SSE           bool   // answer requests as text/event-stream (priming event, a notification, the response in two data lines)
	Sessions      bool   // hand out a session id at initialize and require it afterwards (400 missing, 404 forgotten)
	Token         string // require "Authorization: Bearer <Token>" (401 otherwise)
	ExpireCalls   bool   // every tools/call answers 404: the session is gone
	AskBack       bool   // SSE: a server request with the same id as the client's call comes before the response
	RefuseReplies bool   // a client response (an answer to a server request) gets 500 instead of 202

	mu      sync.Mutex
	session string
	Inits   int         // initialize requests answered
	Deletes int         // DELETE requests seen
	Headers http.Header // of the last request
	Methods []string    // methods of the POSTed messages, in order ("" = a client response)
}

// NewHTTP returns an HTTP fake with the given tools.
func NewHTTP(tools ...Tool) *HTTPServer { return &HTTPServer{Server: New(tools...)} }

// Forget expires the current session: its next request gets 404.
func (h *HTTPServer) Forget() {
	h.mu.Lock()
	h.session = ""
	h.mu.Unlock()
}

func (h *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.Headers = r.Header.Clone()
	session := h.session
	h.mu.Unlock()
	if h.Token != "" && r.Header.Get("Authorization") != "Bearer "+h.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodDelete {
		h.mu.Lock()
		h.Deletes++
		h.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	json.Unmarshal(body, &m)
	h.mu.Lock()
	h.Methods = append(h.Methods, m.Method)
	h.mu.Unlock()
	if m.Method == "" && len(m.ID) > 0 { // a response of the client
		if h.RefuseReplies {
			http.Error(w, "no thanks", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if h.Sessions && m.Method != "initialize" {
		switch sid := r.Header.Get("Mcp-Session-Id"); {
		case sid == "":
			http.Error(w, "session id required", http.StatusBadRequest)
			return
		case sid != session:
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
	}
	if m.Method == "tools/call" && h.ExpireCalls {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if m.Method == "tools/call" && h.Silent {
		<-r.Context().Done()
		return
	}
	reply, err := h.handle(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if m.Method == "initialize" {
		h.mu.Lock()
		h.Inits++
		if h.Sessions {
			h.session = fmt.Sprintf("s-%d", h.Inits)
			w.Header().Set("Mcp-Session-Id", h.session)
		}
		h.mu.Unlock()
	}
	if reply == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if !h.SSE {
		w.Header().Set("Content-Type", "application/json")
		w.Write(reply)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	head, rest, _ := bytes.Cut(reply, []byte(","))
	if h.AskBack && m.Method == "tools/call" {
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"roots/list\"}\n\n", m.ID)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	fmt.Fprintf(w, "id: 1\ndata: \n\n: keep-alive\n\nevent: message\ndata: %s\n\ndata: %s,\ndata: %s\n\n",
		`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":"working"}}`, head, rest)
}
