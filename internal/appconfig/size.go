package appconfig

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = map[string]int64{
	"":    1,
	"b":   1,
	"k":   1000,
	"kb":  1000,
	"m":   1000 * 1000,
	"mb":  1000 * 1000,
	"g":   1000 * 1000 * 1000,
	"gb":  1000 * 1000 * 1000,
	"ki":  1024,
	"kib": 1024,
	"mi":  1024 * 1024,
	"mib": 1024 * 1024,
	"gi":  1024 * 1024 * 1024,
	"gib": 1024 * 1024 * 1024,
}

func ParseSize(s string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("size required")
	}

	split := len(raw)
	for i, r := range raw {
		if (r < '0' || r > '9') && r != '.' {
			split = i
			break
		}
	}

	number := strings.TrimSpace(raw[:split])
	unit := strings.ToLower(strings.TrimSpace(raw[split:]))
	if number == "" {
		return 0, fmt.Errorf("invalid size %q", s)
	}

	value, err := strconv.ParseFloat(number, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}

	multiplier, ok := sizeUnits[unit]
	if !ok {
		return 0, fmt.Errorf("invalid size unit %q", unit)
	}
	return int64(value * float64(multiplier)), nil
}
