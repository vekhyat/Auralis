package main

import (
	"context"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) FrontendReady() {
	a.close.ready()
}
func (a *App) FinishClose(saved bool) {
	if a.close.complete(saved) {
		runtime.Quit(a.ctx)
	}
}
func (a *App) beforeClose(ctx context.Context) bool {
	switch a.close.request() {
	case allowClose:
		return false
	case flushBeforeClose:
		runtime.EventsEmit(ctx, "app:before-close")
	}
	return true
}
