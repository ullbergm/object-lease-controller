package util

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ParseFlexibleDuration parses flexible TTL like "4h", "2d", "1w"
func ParseFlexibleDuration(val string) (time.Duration, error) {
	neg := false
	if len(val) > 0 && val[0] == '-' {
		neg = true
		val = val[1:]
	}

	re := regexp.MustCompile(`(\d*\.\d+|\d+)[^\d]*`)
	unitMap := map[string]time.Duration{
		"d": 24,
		"D": 24,
		"w": 7 * 24,
		"W": 7 * 24,
		"M": 30 * 24,
		"y": 365 * 24,
		"Y": 365 * 24,
	}

	strs := re.FindAllString(val, -1)
	if len(strs) == 0 {
		return 0, fmt.Errorf("invalid duration string: %q", val)
	}
	var sumDur time.Duration
	for _, str := range strs {
		str = strings.TrimSpace(str)
		var _hours time.Duration = 1
		for unit, hours := range unitMap {
			if strings.Contains(str, unit) {
				str = strings.ReplaceAll(str, unit, "h")
				_hours = hours
				break
			}
		}

		dur, err := time.ParseDuration(str)
		if err != nil {
			return 0, err
		}

		sumDur += dur * _hours
	}

	if neg {
		sumDur = -sumDur
	}

	return sumDur, nil
}
