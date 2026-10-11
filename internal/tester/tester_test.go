package tester

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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

// flakyClient fails the first failures requests to every host and counts
// how many requests each host received; hosts are probed concurrently.
func flakyClient(failures int, calls map[string]int) *http.Client {
	var mu sync.Mutex
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Host]++
		attempt := calls[req.URL.Host]
		mu.Unlock()
		if attempt <= failures {
			return nil, errors.New("connection reset")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
			Request:    req,
		}, nil
	})}
}

func TestTestToleratesOneDroppedConnection(t *testing.T) {
	calls := map[string]int{}
	result := Tester{Client: flakyClient(1, calls)}.Test(context.Background())

	if !result.Discord || !result.YouTube || result.Err != nil {
		t.Fatalf("result = %+v, want both reachable after one retry", result)
	}
	if calls["discord.com"] != probeAttempts {
		t.Fatalf("discord.com probed %d times, want %d", calls["discord.com"], probeAttempts)
	}
}

func TestTestStopsOnceQuorumIsOutOfReach(t *testing.T) {
	calls := map[string]int{}
	result := Tester{Client: flakyClient(probeAttempts, calls)}.Test(context.Background())

	if result.Discord || result.YouTube || result.Err == nil {
		t.Fatalf("result = %+v, want both failed", result)
	}
	if want := probeAttempts - probeQuorum + 1; calls["www.youtube.com"] != want {
		t.Fatalf("www.youtube.com probed %d times, want %d", calls["www.youtube.com"], want)
	}
}

func TestCheckReadsOnlyProbeLimit(t *testing.T) {
	// Anything read past the limit hits a broken stream, as a throttled
	// connection would; the probe must not need it.
	body := io.MultiReader(strings.NewReader(strings.Repeat("a", probeBodyLimit)), errReader{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Header: http.Header{}, Request: req}, nil
	})}

	if ok, err := (Tester{}).check(context.Background(), client, discordURL); !ok || err != nil {
		t.Fatalf("check() = %v, %v, want success without reading past the limit", ok, err)
	}
}

func TestDefaultClientRequestsUncompressedBodies(t *testing.T) {
	client, closeClient := New(0).client()
	defer closeClient()

	transport := client.Transport.(*http.Transport)
	if !transport.DisableCompression || !transport.DisableKeepAlives {
		t.Fatalf("transport compression disabled = %v, keep-alives disabled = %v, want both", transport.DisableCompression, transport.DisableKeepAlives)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stream stalled") }
