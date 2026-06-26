package transfer

import (
	"fmt"
	"time"
)

type ProgressInfo struct {
	File        string `json:"file"`
	Percent     int    `json:"percent"`
	Transferred int64  `json:"transferred"`
	Total       int64  `json:"total"`
	Speed       string `json:"speed"`
}

type ProgressFunc func(info ProgressInfo)

type ProgressTracker struct {
	file        string
	total       int64
	transferred int64
	lastReport  time.Time
	lastPercent int
	startTime   time.Time
	callback    ProgressFunc
}

func NewProgressTracker(file string, total int64, callback ProgressFunc) *ProgressTracker {
	return &ProgressTracker{
		file:      file,
		total:     total,
		callback:  callback,
		startTime: time.Now(),
	}
}

func (t *ProgressTracker) Update(n int) {
	if t.callback == nil || t.total <= 0 {
		return
	}

	t.transferred += int64(n)
	now := time.Now()
	percent := int(t.transferred * 100 / t.total)

	if percent-t.lastPercent >= 10 || now.Sub(t.lastReport) >= 5*time.Second {
		t.lastReport = now
		t.lastPercent = percent
		elapsed := now.Sub(t.startTime).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(t.transferred) / elapsed
		}
		t.callback(ProgressInfo{
			File:        t.file,
			Percent:     percent,
			Transferred: t.transferred,
			Total:       t.total,
			Speed:       humanSize(int64(speed)) + "/s",
		})
	}
}

func (t *ProgressTracker) Finish() {
	if t.callback == nil || t.total <= 0 {
		return
	}
	elapsed := time.Since(t.startTime).Seconds()
	speed := float64(0)
	if elapsed > 0 {
		speed = float64(t.transferred) / elapsed
	}
	t.callback(ProgressInfo{
		File:        t.file,
		Percent:     100,
		Transferred: t.transferred,
		Total:       t.total,
		Speed:       humanSize(int64(speed)) + "/s",
	})
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func HumanSize(b int64) string { return humanSize(b) }
