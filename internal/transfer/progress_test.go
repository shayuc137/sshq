package transfer

import "testing"

// Small files complete in a single write that already hits 100% in Update();
// Finish() must not emit a second 100% report (the duplicate-progress bug).
func TestProgressTracker_SmallFileNoDoubleReport(t *testing.T) {
	var reports100 int
	tracker := NewProgressTracker("small.txt", 100, func(info ProgressInfo) {
		if info.Percent == 100 {
			reports100++
		}
	})
	tracker.Update(100)
	tracker.Finish()
	if reports100 != 1 {
		t.Errorf("expected exactly one 100%% report, got %d", reports100)
	}
}

// When Update() never reaches 100% (reports at 90% then stops), Finish() is
// responsible for the single closing 100% report.
func TestProgressTracker_LargeFileFinishReportsOnce(t *testing.T) {
	var reports100 int
	tracker := NewProgressTracker("big.bin", 1000, func(info ProgressInfo) {
		if info.Percent == 100 {
			reports100++
		}
	})
	tracker.Update(900)
	tracker.Finish()
	if reports100 != 1 {
		t.Errorf("expected exactly one 100%% report from Finish, got %d", reports100)
	}
}
