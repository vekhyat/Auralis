package main

import "sync"

type closeState uint8

const (
	closeCold closeState = iota
	closeReady
	closeSaving
	closeApproved
)

type closeAction uint8

const (
	allowClose closeAction = iota
	flushBeforeClose
	waitForCloseFlush
)

type closeGate struct {
	mu    sync.Mutex
	state closeState
}

func (g *closeGate) ready() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == closeCold {
		g.state = closeReady
	}
}
func (g *closeGate) request() closeAction {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch g.state {
	case closeCold, closeApproved:
		return allowClose
	case closeReady:
		g.state = closeSaving
		return flushBeforeClose
	default:
		return waitForCloseFlush
	}
}
func (g *closeGate) complete(saved bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != closeSaving {
		return false
	}
	if saved {
		g.state = closeApproved
	} else {
		g.state = closeReady
	}
	return saved
}
