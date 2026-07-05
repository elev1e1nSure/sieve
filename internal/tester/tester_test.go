package tester

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc lets a test control what each probed URL returns without
// touching the network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubClient(status map[string]int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		code, ok := status[req.URL.Host]
		if !ok {
			return nil, errors.New("unreachable: " + req.URL.Host)
		}
		return &http.Response{
			StatusCode: code,
			Status:     http.StatusText(code),
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
			Request:    req,
		}, nil
	})}
}

func TestTestBothReachable(t *testing.T) {
	tester := Tester{Client: stubClient(map[string]int{
		"discord.com":     http.StatusOK,
		"www.youtube.com": http.StatusMovedPermanently,
	})}

	result := tester.Test(context.Background())
	if !result.Discord || !result.YouTube || result.Err != nil {
		t.Fatalf("result = %+v, want both reachable", result)
	}
}

func TestTestPartialFailure(t *testing.T) {
	tester := Tester{Client: stubClient(map[string]int{
		"discord.com":     http.StatusOK,
		"www.youtube.com": http.StatusForbidden,
	})}

	result := tester.Test(context.Background())
	if !result.Discord || result.YouTube {
		t.Fatalf("result = %+v, want discord ok / youtube blocked", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "youtube") {
		t.Fatalf("Err = %v, want youtube failure attributed", result.Err)
	}
}

func TestTestTransportError(t *testing.T) {
	tester := Tester{Client: stubClient(nil)} // every host unreachable

	result := tester.Test(context.Background())
	if result.Discord || result.YouTube || result.Err == nil {
		t.Fatalf("result = %+v, want both failed with error", result)
	}
}

func TestNewDefaultTimeout(t *testing.T) {
	if got := New(0).Timeout; got != defaultTimeout {
		t.Fatalf("Timeout = %v, want default %v", got, defaultTimeout)
	}
	if got := New(-1).Timeout; got != defaultTimeout {
		t.Fatalf("Timeout = %v, want default %v", got, defaultTimeout)
	}
}
