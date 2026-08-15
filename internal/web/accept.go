package web

import (
	"math"
	"mime"
	"slices"
	"strconv"
	"strings"
)

type mediaPreference struct {
	quality     float64
	specificity int
	present     bool
}

type accountRepresentation uint8

const (
	representationNotAcceptable accountRepresentation = iota
	representationJSON
	representationHTML
	representationImage
)

// negotiateAccountRepresentation selects among the representations exposed by
// the account endpoint. An explicit Accept preference is authoritative. When
// the header is absent or leaves multiple representations equally preferred,
// the user agent breaks the tie: browsers receive HTML and known API clients
// (or clients without a user agent) retain the historical JSON behavior.
func negotiateAccountRepresentation(
	values []string,
	userAgent string,
) accountRepresentation {
	items := splitAcceptValues(values)
	if len(items) == 0 {
		return contextualAccountRepresentation(userAgent)
	}

	var html mediaPreference
	var jsonPreference mediaPreference
	var image mediaPreference
	for _, item := range items {
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

		switch mediaType {
		case "text/html":
			html = mostSpecificPreference(html, mediaPreference{quality, 2, true})
		case "text/*":
			html = mostSpecificPreference(html, mediaPreference{quality, 1, true})
		case "application/json":
			jsonPreference = mostSpecificPreference(
				jsonPreference,
				mediaPreference{quality, 2, true},
			)
		case "application/*":
			jsonPreference = mostSpecificPreference(
				jsonPreference,
				mediaPreference{quality, 1, true},
			)
		case "image/*":
			image = mostSpecificPreference(image, mediaPreference{quality, 1, true})
		case "*/*":
			wildcard := mediaPreference{quality, 0, true}
			html = mostSpecificPreference(html, wildcard)
			jsonPreference = mostSpecificPreference(jsonPreference, wildcard)
			image = mostSpecificPreference(image, wildcard)
		default:
			if strings.HasPrefix(mediaType, "image/") {
				image = mostSpecificPreference(
					image,
					mediaPreference{quality, 2, true},
				)
			}
		}
	}

	preferences := []struct {
		representation accountRepresentation
		preference     mediaPreference
	}{
		{representationJSON, jsonPreference},
		{representationHTML, html},
		{representationImage, image},
	}
	best := mediaPreference{}
	var candidates []accountRepresentation
	for _, item := range preferences {
		preference := item.preference
		if !preference.present || preference.quality == 0 {
			continue
		}
		switch {
		case !best.present || preference.quality > best.quality ||
			(preference.quality == best.quality && preference.specificity > best.specificity):
			best = preference
			candidates = []accountRepresentation{item.representation}
		case preference.quality == best.quality && preference.specificity == best.specificity:
			candidates = append(candidates, item.representation)
		}
	}
	if len(candidates) == 0 {
		return representationNotAcceptable
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	contextual := contextualAccountRepresentation(userAgent)
	if slices.Contains(candidates, contextual) {
		return contextual
	}
	if slices.Contains(candidates, representationHTML) {
		return representationHTML
	}
	if slices.Contains(candidates, representationJSON) {
		return representationJSON
	}
	return representationImage
}

func contextualAccountRepresentation(userAgent string) accountRepresentation {
	if userAgent == "" || isProgrammaticUserAgent(userAgent) {
		return representationJSON
	}
	if isBrowserUserAgent(userAgent) {
		return representationHTML
	}
	// Preserve the endpoint's historical API behavior for unrecognized agents.
	return representationJSON
}

func isProgrammaticUserAgent(userAgent string) bool {
	lower := strings.ToLower(userAgent)
	for _, marker := range []string{
		"aiohttp/",
		"axios/",
		"curl/",
		"go-http-client/",
		"got/",
		"httpie/",
		"httpx/",
		"insomnia/",
		"java/",
		"libwww-perl/",
		"node-fetch",
		"okhttp/",
		"postmanruntime/",
		"powershell/",
		"python-requests/",
		"python-urllib/",
		"reqwest/",
		"restsharp/",
		"undici",
		"wget/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return lower == "node"
}

func isBrowserUserAgent(userAgent string) bool {
	lower := strings.ToLower(userAgent)
	return strings.Contains(lower, "mozilla/") ||
		strings.Contains(lower, "opera/") ||
		strings.Contains(lower, "lynx/") ||
		strings.Contains(lower, "elinks/") ||
		strings.Contains(lower, "w3m/")
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
