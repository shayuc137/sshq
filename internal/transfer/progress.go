package transfer

import (
	"time"

	"github.com/shayuc137/sshq/internal/humanize"
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
	finished    bool
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
		// Mark finished here so Finish() won't emit a duplicate 100% report.
		if percent >= 100 {
			t.finished = true
		}
		t.callback(ProgressInfo{
			File:        t.file,
			Percent:     percent,
			Transferred: t.transferred,
			Total:       t.total,
			Speed:       humanize.Bytes(int64(speed)) + "/s",
		})
	}
}

func (t *ProgressTracker) Finish() {
	if t.callback == nil || t.total <= 0 || t.finished {
		return
	}
	t.finished = true
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
		Speed:       humanize.Bytes(int64(speed)) + "/s",
	})
}
