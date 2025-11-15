package util

import (
	"fmt"
	"regexp"
	"strconv"
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

	re := regexp.MustCompile(`(\d*\.\d+|\d+)([A-Za-zµμ]*)`)

	// Unit map: map flexible shorthand to concrete time.Duration values.
	unitMap := map[string]time.Duration{
		"d":   24 * time.Hour,
		"w":   7 * 24 * time.Hour,
		"mth": 30 * 24 * time.Hour, // month (m is minutes for time.ParseDuration so use mth internally)
		"y":   365 * 24 * time.Hour,
	}

	strs := re.FindAllStringSubmatch(val, -1)
	if len(strs) == 0 {
		return 0, fmt.Errorf("invalid duration string: %q", val)
	}
	var sumDur time.Duration
	for _, m := range strs {
		// m[0] full match, m[1] numeric part, m[2] unit part
		numStr := m[1]
		unitStr := strings.TrimSpace(m[2])

		if unitStr == "" {
			// default to seconds if unit omitted? We choose to error since ambiguous
			return 0, fmt.Errorf("missing unit in duration element: %q", m[0])
		}

		// Normalize common micro symbol variations
		unitStr = strings.ReplaceAll(unitStr, "µ", "u")
		unitStr = strings.ReplaceAll(unitStr, "μ", "u")

		// Treat units case-insensitively by normalizing to lower-case. Use
		// 'mo' (or 'mth'/'month') for months to avoid conflicting with 'm' (minutes)
		uLower := strings.ToLower(unitStr)

		switch uLower {
		case "ns", "us", "ms", "s", "m", "h":
			// safe to delegate to time.ParseDuration
			dur, err := time.ParseDuration(numStr + uLower)
			if err != nil {
				return 0, err
			}
			sumDur += dur
			continue
		}

		// Custom units: days, weeks, months, years
		l := strings.ToLower(unitStr)
		var unitDur time.Duration
		switch l {
		case "d":
			unitDur = unitMap["d"]
		case "w":
			unitDur = unitMap["w"]
		case "mth":
			// 'mth' is a month alias. 'm' is reserved for minutes and handled by time.ParseDuration above.
			unitDur = unitMap["mth"]
		case "month":
			unitDur = unitMap["mth"]
		case "mo":
			unitDur = unitMap["mth"]
		case "y":
			unitDur = unitMap["y"]
		default:
			return 0, fmt.Errorf("unknown duration unit: %q", unitStr)
		}

		// Convert numeric part to float so we can support fractions like 1.5d
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		part := time.Duration(float64(unitDur) * num)
		sumDur += part
	}

	if neg {
		sumDur = -sumDur
	}

	return sumDur, nil
}
