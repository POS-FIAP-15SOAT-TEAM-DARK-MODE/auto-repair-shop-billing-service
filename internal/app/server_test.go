package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewHTTPServer(t *testing.T) {
	s := NewHTTPServer("test", ":0", http.DefaultServeMux)
	assert.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
}

func TestHTTPServer_Start_Stop(t *testing.T) {
	s := NewHTTPServer("test", ":0", http.DefaultServeMux)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err := s.Stop(stopCtx)
	assert.NoError(t, err)

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

func TestHTTPServer_Name(t *testing.T) {
	s := NewHTTPServer("my-server", ":8080", http.DefaultServeMux)
	assert.Equal(t, "my-server", s.Name())
}

func TestHTTPServer_Start_InvalidAddr(t *testing.T) {
	s := NewHTTPServer("test", "invalid-addr", http.DefaultServeMux)

	ctx := context.Background()
	err := s.Start(ctx)
	assert.Error(t, err)
}
