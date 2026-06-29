package tester

import "time"

type Tester struct {
	Timeout time.Duration
}

func New(timeout time.Duration) Tester {
	return Tester{Timeout: timeout}
}
