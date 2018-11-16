package port

import (
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/pkg/errors"
)

// RequestModifierFunc is used to transform a simple function as a RequestModifier
type RequestModifierFunc func(req *http.Request) error

// Intercept modifies the request with the RequestModifierFunc function
func (r RequestModifierFunc) Intercept(req *http.Request) error {
	return r(req)
}

// RequestModifier is invoked by RequestInterceptor to modify the original request
type RequestModifier interface {
	Intercept(req *http.Request) error
}

// NewRequestInterceptor returns a roundtripper that adds the service key
// on every request
func NewRequestInterceptor(baseTransport http.RoundTripper, modifier RequestModifier) *RequestIntercepter {
	t := baseTransport
	if t == nil {
		t = http.DefaultTransport
	}
	return &RequestIntercepter{
		requestModifier: modifier,
		Base:            t,
	}
}

// RequestIntercepter adds the knocker service key on every request
// most of this code has been taken from net/oauth2
// @see https://github.com/golang/oauth2/blob/master/transport.go
type RequestIntercepter struct {
	requestModifier RequestModifier
	Base            http.RoundTripper
	mu              sync.Mutex                      // guards modReq
	modReq          map[*http.Request]*http.Request // original -> modified
}

// RoundTrip process the current request before sending it to the real HTTP layer
func (k *RequestIntercepter) RoundTrip(req *http.Request) (res *http.Response, err error) {
	reqBodyClosed := false
	if req.Body != nil {
		defer func() {
			if !reqBodyClosed {
				_ = req.Body.Close()
			}
		}()
	}

	req2 := cloneRequest(req) // per RoundTripper contract

	// modify the copied request
	err = k.requestModifier.Intercept(req2)
	if err != nil {
		return nil, errors.Wrap(err, "error while intercepting request")
	}

	k.setModReq(req, req2)
	res, err = k.base().RoundTrip(req2)

	// req.Body is assumed to have been closed by the base RoundTripper.
	reqBodyClosed = true

	if err != nil {
		k.setModReq(req, nil)
		return nil, err
	}
	res.Body = &onEOFReader{
		rc: res.Body,
		fn: func() { k.setModReq(req, nil) },
	}
	return res, nil
}

func cloneRequest(r *http.Request) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r
	// deep copy of the Header
	r2.Header = make(http.Header, len(r.Header))
	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}
	// Deep copy the URL because it isn't
	// a map and the URL is mutable by users
	// of Intercept.
	if r.URL != nil {
		r2URL := new(url.URL)
		*r2URL = *r.URL
		r2.URL = r2URL
	}
	return r2
}

// CancelRequest cancels an in-flight request by closing its connection.
// @deprecated use context instead
func (k *RequestIntercepter) CancelRequest(req *http.Request) {
	type canceler interface {
		CancelRequest(*http.Request)
	}
	if cr, ok := k.base().(canceler); ok {
		k.mu.Lock()
		modReq := k.modReq[req]
		delete(k.modReq, req)
		k.mu.Unlock()
		cr.CancelRequest(modReq)
	}
}

func (k *RequestIntercepter) setModReq(orig, mod *http.Request) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.modReq == nil {
		k.modReq = make(map[*http.Request]*http.Request)
	}
	if mod == nil {
		delete(k.modReq, orig)
	} else {
		k.modReq[orig] = mod
	}
}

func (k *RequestIntercepter) base() http.RoundTripper {
	if k.Base != nil {
		return k.Base
	}
	return http.DefaultTransport
}

type onEOFReader struct {
	rc io.ReadCloser
	fn func()
}

func (r *onEOFReader) Read(p []byte) (n int, err error) {
	n, err = r.rc.Read(p)
	if err == io.EOF {
		r.runFunc()
	}
	return
}

func (r *onEOFReader) Close() error {
	err := r.rc.Close()
	r.runFunc()
	return err
}

func (r *onEOFReader) runFunc() {
	if fn := r.fn; fn != nil {
		fn()
		r.fn = nil
	}
}
