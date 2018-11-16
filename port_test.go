package port

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestIntercepter_RoundTrip(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i := r.Header.Get("intercepted"); i != "true" {
			t.Error("unable to get the intercepted header, the request has not been intercepted")
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer func() {
		s.Close()
	}()

	c := s.Client()

	c.Transport = NewRequestInterceptor(c.Transport, RequestModifierFunc(func(r *http.Request) error {
		r.Header.Set("intercepted", "true")
		return nil
	}))

	_, err := c.Get(s.URL)
	require.NoError(t, err)
}

func TestRequestIntercepter_RoundTrip_Request_Cancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
	}))

	defer func() {
		s.Close()
	}()

	c := s.Client()

	c.Transport = NewRequestInterceptor(c.Transport, RequestModifierFunc(func(r *http.Request) error {
		r.Header.Set("intercepted", "true")
		return nil
	}))

	req, err := http.NewRequest("GET", s.URL, nil)
	cctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	st := time.Now()
	req = req.WithContext(cctx)
	require.NoError(t, err)
	_, err = c.Do(req)

	require.NotNil(t, err)
	require.True(t, strings.Contains(err.Error(), "context deadline exceeded"))
	assert.WithinDuration(t, time.Now(), st, 105*time.Millisecond)

}
