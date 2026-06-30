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

	return Tester{
		Timeout: timeout,
		Client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (t Tester) Test(ctx context.Context) TestResult {
	discordOK, discordErr := t.check(ctx, discordURL)
	youtubeOK, youtubeErr := t.check(ctx, youtubeURL)

	return TestResult{
		Discord: discordOK,
		YouTube: youtubeOK,
		Err:     joinErrors(discordErr, youtubeErr),
	}
}

func (t Tester) check(ctx context.Context, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "sieve")

	resp, err := t.client().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, nil
	}

	return false, fmt.Errorf("%s returned %s", url, resp.Status)
}

func (t Tester) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}

	return &http.Client{Timeout: t.Timeout}
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
