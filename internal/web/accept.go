package web

import (
	"math"
	"mime"
	"strconv"
	"strings"
)

type mediaPreference struct {
	quality     float64
	specificity int
	present     bool
}

func prefersImage(values []string) bool {
	var image mediaPreference
	var jsonPreference mediaPreference
	for _, item := range splitAcceptValues(values) {
		mediaType, parameters, err := mime.ParseMediaType(item)
		if err != nil {
			continue
		}
		quality := 1.0
		if raw, ok := parameters["q"]; ok {
			quality, err = strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(quality) || quality < 0 || quality > 1 {
				continue
			}
		}

		switch {
		case strings.HasPrefix(mediaType, "image/"):
			specificity := 2
			if mediaType == "image/*" {
				specificity = 1
			}
			image = betterPreference(image, mediaPreference{quality, specificity, true})
		case mediaType == "application/json":
			jsonPreference = mostSpecificPreference(
				jsonPreference,
				mediaPreference{quality, 2, true},
			)
		case mediaType == "application/*":
			jsonPreference = mostSpecificPreference(
				jsonPreference,
				mediaPreference{quality, 1, true},
			)
		case mediaType == "*/*":
			jsonPreference = mostSpecificPreference(
				jsonPreference,
				mediaPreference{quality, 0, true},
			)
		}
	}
	if !image.present || image.quality == 0 {
		return false
	}
	if !jsonPreference.present {
		return true
	}
	if image.quality != jsonPreference.quality {
		return image.quality > jsonPreference.quality
	}
	return image.specificity > jsonPreference.specificity
}

func betterPreference(current, candidate mediaPreference) mediaPreference {
	if !current.present || candidate.quality > current.quality ||
		(candidate.quality == current.quality && candidate.specificity > current.specificity) {
		return candidate
	}
	return current
}

func mostSpecificPreference(current, candidate mediaPreference) mediaPreference {
	if !current.present || candidate.specificity > current.specificity ||
		(candidate.specificity == current.specificity && candidate.quality > current.quality) {
		return candidate
	}
	return current
}

func splitAcceptValues(values []string) []string {
	var result []string
	for _, value := range values {
		start := 0
		quoted := false
		escaped := false
		for index := range len(value) {
			character := value[index]
			switch {
			case escaped:
				escaped = false
			case quoted && character == '\\':
				escaped = true
			case character == '"':
				quoted = !quoted
			case character == ',' && !quoted:
				if item := strings.TrimSpace(value[start:index]); item != "" {
					result = append(result, item)
				}
				start = index + 1
			}
		}
		if item := strings.TrimSpace(value[start:]); item != "" {
			result = append(result, item)
		}
	}
	return result
}
