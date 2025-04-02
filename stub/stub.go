package stub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mashiike/simplemqhttp/simplemq"
)

// Server represents a stub server for testing
type Server struct {
	server   *httptest.Server
	messages map[string]map[string]*simplemq.Message // queue -> message_id -> message
	counter  int
	mu       sync.Mutex
	apiKey   string
}

// NewServer creates a new stub server
func NewServer(apiKey string) *Server {
	s := &Server{
		messages: make(map[string]map[string]*simplemq.Message),
		apiKey:   apiKey,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/queues/", s.handleRequests)

	s.server = httptest.NewServer(http.HandlerFunc(s.authMiddleware(mux)))

	return s
}

// URL returns the server URL
func (s *Server) URL() string {
	return s.server.URL
}

// Close closes the server
func (s *Server) Close() {
	s.server.Close()
}

// Reset clears all messages and errors
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = make(map[string]map[string]*simplemq.Message)
	s.counter = 0
}

// AddMessage adds a message to a queue for testing
func (s *Server) AddMessage(queue, content string) *simplemq.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.messages[queue]; !ok {
		s.messages[queue] = make(map[string]*simplemq.Message)
	}

	s.counter++
	now := time.Now().UnixMilli()
	id := uuid.New().String()
	msg := &simplemq.Message{
		ID:        id,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.messages[queue][id] = msg
	return msg
}

// GetMessage gets a message by ID and queue
func (s *Server) GetMessage(queue, id string) *simplemq.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	if queueMsgs, ok := s.messages[queue]; ok {
		return queueMsgs[id]
	}
	return nil
}

// GetQueueSize returns the number of messages in a queue
func (s *Server) GetQueueSize(queue string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if queueMsgs, ok := s.messages[queue]; ok {
		return len(queueMsgs)
	}
	return 0
}

// authMiddleware verifies API key
func (s *Server) authMiddleware(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		expected := "Bearer " + s.apiKey

		if authHeader != expected {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(simplemq.APIError{
				Code:    401,
				Message: "unauthorized",
			})
			return
		}

		next.ServeHTTP(w, r)
	}
}

// handleRequests routes the request to the appropriate handler based on the URL path and method
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	// URL patterns to extract parameters
	queueMessagesPattern := regexp.MustCompile(`^/v1/queues/([^/]+)/messages$`)
	queueMessageIDPattern := regexp.MustCompile(`^/v1/queues/([^/]+)/messages/([^/]+)$`)

	path := r.URL.Path

	// Route to the appropriate handler
	if queueMessagesPattern.MatchString(path) {
		matches := queueMessagesPattern.FindStringSubmatch(path)
		queue := matches[1]

		switch r.Method {
		case http.MethodPost:
			s.handleSendMessage(w, r, queue)
		case http.MethodGet:
			s.handleReceiveMessages(w, r, queue)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	if queueMessageIDPattern.MatchString(path) {
		matches := queueMessageIDPattern.FindStringSubmatch(path)
		queue, id := matches[1], matches[2]

		switch r.Method {
		case http.MethodDelete:
			s.handleDeleteMessage(w, r, queue, id)
		case http.MethodPut:
			s.handleExtendVisibility(w, r, queue, id)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	// No matching route
	w.WriteHeader(http.StatusNotFound)
}

// handleSendMessage handles POST /v1/queues/{queue}/messages
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request, queue string) {
	var reqBody struct {
		Content string `json:"content"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(simplemq.APIError{
			Code:    400,
			Message: "Invalid request body",
		})
		return
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(simplemq.APIError{
			Code:    400,
			Message: "Invalid JSON",
		})
		return
	}

	msg := s.AddMessage(queue, reqBody.Content)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Message *simplemq.Message `json:"message"`
	}{
		Message: msg,
	})
}

// handleReceiveMessages handles GET /v1/queues/{queue}/messages
func (s *Server) handleReceiveMessages(w http.ResponseWriter, _ *http.Request, queue string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := []*simplemq.Message{}
	now := time.Now().UnixMilli()

	if queueMsgs, ok := s.messages[queue]; ok {
		for _, msg := range queueMsgs {
			if msg.VisibilityTimeoutAt < now {
				messages = append(messages, msg)
				msg.VisibilityTimeoutAt = now + 30000
				msg.AcquiredAt = now
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Result   string              `json:"result"`
		Messages []*simplemq.Message `json:"messages"`
	}{
		Result:   "success",
		Messages: messages,
	})
}

// handleDeleteMessage handles DELETE /v1/queues/{queue}/messages/{id}
func (s *Server) handleDeleteMessage(w http.ResponseWriter, _ *http.Request, queue, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if queueMsgs, ok := s.messages[queue]; ok {
		if _, exists := queueMsgs[id]; exists {
			delete(queueMsgs, id)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(simplemq.APIError{
		Code:    404,
		Message: "Message not found",
	})
}

// handleExtendVisibility handles PUT /v1/queues/{queue}/messages/{id}
func (s *Server) handleExtendVisibility(w http.ResponseWriter, _ *http.Request, queue, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if queueMsgs, ok := s.messages[queue]; ok {
		if msg, exists := queueMsgs[id]; exists {
			if msg.VisibilityTimeoutAt > time.Now().UnixMilli() {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(simplemq.APIError{
					Code:    409,
					Message: "Message is already acquired",
				})
				return
			}
			msg.VisibilityTimeoutAt += 30000
			s.messages[queue][id] = msg
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Result  string            `json:"result"`
				Message *simplemq.Message `json:"message"`
			}{
				Result:  "success",
				Message: msg,
			})
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(simplemq.APIError{
		Code:    404,
		Message: "Message not found",
	})
}
