// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ws "github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

// testServer is an httptest websocket server that records every binary
// message it receives. Stall makes every connection stop reading after the
// next message, which is how the conformance tests model a backend that
// stopped answering (AT-4).
// It uses only the standard library and gorilla/websocket, which this
// module already requires.
type testServer struct {
	srv *httptest.Server

	stallOnce sync.Once
	stalled   chan struct{}
	released  chan struct{}

	mu       sync.Mutex
	messages [][]byte
	notify   chan struct{}
}

// newTestServer starts a websocket server that lives until t ends.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	s := &testServer{
		stalled:  make(chan struct{}),
		released: make(chan struct{}),
		notify:   make(chan struct{}, 1),
	}
	upgrader := ws.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			select {
			case <-s.stalled:
				// Stop reading until the test ends: the client's writes
				// and its close get no answer.
				<-s.released
				return
			default:
			}
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, payload)
			s.mu.Unlock()
			select {
			case s.notify <- struct{}{}:
			default:
			}
		}
	}))
	t.Cleanup(func() {
		close(s.released)
		s.srv.CloseClientConnections()
		s.srv.Close()
	})
	return s
}

// URL returns the server's ws:// address.
func (s *testServer) URL() string {
	return "ws" + strings.TrimPrefix(s.srv.URL, "http")
}

// Stall makes every connection stop reading after the next message: the
// handler checks the stall between messages, so a handler already blocked
// in ReadMessage reads one more message before it stops. It is safe to call
// more than once.
func (s *testServer) Stall(*testing.T) {
	s.stallOnce.Do(func() { close(s.stalled) })
}

// await returns nil once the server has received a message that unmarshals
// into a value proto.Equal to want, or ctx's error.
func (s *testServer) await(ctx context.Context, want proto.Message) error {
	for {
		if s.received(want) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.notify:
		}
	}
}

func (s *testServer) received(want proto.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, payload := range s.messages {
		got := want.ProtoReflect().New().Interface()
		if proto.Unmarshal(payload, got) == nil && proto.Equal(got, want) {
			return true
		}
	}
	return false
}
