package tester

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultTimeout = 5 * time.Second
	discordURL     = "https://discord.com"
	youtubeURL     = "https://www.youtube.com"
)

type Tester struct {
	Timeout time.Duration
	Client  *http.Client
}

type ConnectivityTester interface {
	Test(ctx context.Context) TestResult
}

type TestResult struct {
	Discord bool
	YouTube bool
	Err     error
}

func New(timeout time.Duration) Tester {
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return Tester{Timeout: timeout}
}

func (t Tester) Test(ctx context.Context) TestResult {
	client, closeClient := t.client()
	defer closeClient()

	type checkResult struct {
		discord bool
		ok      bool
		err     error
	}

	results := make(chan checkResult, 2)
	go func() {
		ok, err := t.check(ctx, client, discordURL)
		results <- checkResult{discord: true, ok: ok, err: err}
	}()
	go func() {
		ok, err := t.check(ctx, client, youtubeURL)
		results <- checkResult{ok: ok, err: err}
	}()

	var discordOK, youtubeOK bool
	var discordErr, youtubeErr error
	for range 2 {
		result := <-results
		if result.discord {
			discordOK = result.ok
			discordErr = result.err
		} else {
			youtubeOK = result.ok
			youtubeErr = result.err
		}
	}

	return TestResult{
		Discord: discordOK,
		YouTube: youtubeOK,
		Err:     joinErrors(discordErr, youtubeErr),
	}
}

func (t Tester) check(ctx context.Context, client *http.Client, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "sieve")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return false, fmt.Errorf("read %s response: %w", url, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, nil
	}

	return false, fmt.Errorf("%s returned %s", url, resp.Status)
}

func (t Tester) client() (*http.Client, func()) {
	if t.Client != nil {
		return t.Client, func() {}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	client := &http.Client{Timeout: t.Timeout, Transport: transport}
	return client, transport.CloseIdleConnections
}

func joinErrors(discordErr, youtubeErr error) error {
	switch {
	case discordErr == nil && youtubeErr == nil:
		return nil
	case discordErr != nil && youtubeErr != nil:
		return fmt.Errorf("discord: %w; youtube: %v", discordErr, youtubeErr)
	case discordErr != nil:
		return fmt.Errorf("discord: %w", discordErr)
	default:
		return fmt.Errorf("youtube: %w", youtubeErr)
	}
}
