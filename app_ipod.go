package main

import (
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend/devices/ipod"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startIPodWatch() {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	if a.ipodStop != nil {
		return
	}
	a.ipodStop = make(chan struct{})
	go a.watchIPods()
}

func (a *App) stopIPodWatch() {
	if a.ipodStop == nil {
		return
	}
	close(a.ipodStop)
	a.ipodStop = nil
}

func (a *App) watchIPods() {
	stop := a.ipodStop
	var previous string
	emit := func() {
		if a.ctx == nil || a.ipods == nil {
			return
		}
		list := a.ipods.List()
		signature := ipodSignature(list)
		if signature == previous {
			return
		}
		previous = signature
		runtime.EventsEmit(a.ctx, "ipod:devices", list)
	}
	emit()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			emit()
		case <-stop:
			return
		}
	}
}

func ipodSignature(list []ipod.Device) string {
	var b strings.Builder
	for _, dev := range list {
		b.WriteString(dev.ID)
		b.WriteByte('|')
		b.WriteString(dev.Mode)
		b.WriteByte('|')
		b.WriteString(dev.Warning)
		b.WriteByte('|')
		b.WriteString(itoa(dev.TrackCount))
		b.WriteByte(';')
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (a *App) ListIPods() ([]ipod.Device, error) {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	return a.ipods.List(), nil
}

func (a *App) SendToIPod(id string, paths []string, format string) (ipod.SendResult, error) {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	return a.ipods.Send(id, paths, format, func(progress ipod.Progress) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "ipod:progress", progress)
		}
	})
}

func (a *App) RemoveFromIPod(id string, trackIDs []int) error {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	return a.ipods.Remove(id, trackIDs)
}

func (a *App) EjectIPod(id string) error {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	return a.ipods.Eject(id)
}

func (a *App) DoctorIPod(id string, action string) (ipod.DoctorReport, error) {
	if a.ipods == nil {
		a.ipods = ipod.NewManager()
	}
	return a.ipods.Doctor(id, action)
}

func (a *App) CancelIPod() {
	if a.ipods != nil {
		a.ipods.Cancel()
	}
}
