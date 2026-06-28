package config

import (
	"encoding/json"
	"testing"
)

// An empty/nil host list must serialize as [] (not null) so agents can always
// iterate over data as an array.
func TestHostList_MarshalJSON_EmptyIsArray(t *testing.T) {
	for name, hl := range map[string]HostList{"nil": nil, "empty": {}} {
		b, err := json.Marshal(hl)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if string(b) != "[]" {
			t.Errorf("%s HostList = %s, want []", name, b)
		}
	}
}

func TestHostList_MarshalJSON_LowercaseFields(t *testing.T) {
	hl := HostList{{Alias: "rn", HostName: "1.2.3.4", User: "shayu", Port: "22"}}
	b, err := json.Marshal(hl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 host, got %d", len(got))
	}
	if got[0]["alias"] != "rn" || got[0]["hostname"] != "1.2.3.4" {
		t.Errorf("expected lowercase alias/hostname fields, got %s", b)
	}
}

func TestHostList_Pretty_Empty(t *testing.T) {
	if got := HostList(nil).Pretty(); got != "No hosts configured.\n" {
		t.Errorf("empty HostList.Pretty() = %q", got)
	}
}
