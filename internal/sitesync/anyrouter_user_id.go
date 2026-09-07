package sitesync

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	anyRouterNumericUnderscoreRE = regexp.MustCompile(`_(\d{4,8})(?:\D|$)`)
	anyRouterNumericKeywordRE    = regexp.MustCompile(`(?:user(?:name)?|uid|id)[^\d]{0,16}(\d{4,8})(?:\D|$)`)
)

func anyRouterExtractLikelyUserIDs(token string) []int {
	sessionValues := make([]string, 0)
	seenSessions := make(map[string]struct{})
	sessionCookiePattern := regexp.MustCompile(`(?:^|;\s*)session=([^;]+)`)
	for _, candidate := range anyRouterBuildCookieCandidates(token) {
		match := sessionCookiePattern.FindStringSubmatch(candidate)
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if _, ok := seenSessions[value]; ok {
			continue
		}
		seenSessions[value] = struct{}{}
		sessionValues = append(sessionValues, value)
	}
	raw := strings.TrimSpace(token)
	if raw != "" && !strings.Contains(raw, "=") {
		raw = strings.TrimPrefix(raw, "Bearer ")
		raw = strings.TrimPrefix(raw, "bearer ")
		if _, ok := seenSessions[raw]; !ok {
			seenSessions[raw] = struct{}{}
			sessionValues = append(sessionValues, raw)
		}
	}

	ids := make([]int, 0)
	seen := make(map[int]struct{})
	appendID := func(value int) {
		if value <= 0 || value > 10_000_000 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}

	for _, sessionValue := range sessionValues {
		decodedBuffer, ok := anyRouterDecodeBase64Buffer(sessionValue)
		if !ok {
			continue
		}

		payloadTexts := []string{string(decodedBuffer)}
		payloadBuffers := [][]byte{decodedBuffer}
		parts := strings.Split(string(decodedBuffer), "|")
		if len(parts) >= 2 {
			if middleBuffer, ok := anyRouterDecodeBase64Buffer(parts[1]); ok {
				payloadTexts = append(payloadTexts, string(middleBuffer))
				payloadBuffers = append(payloadBuffers, middleBuffer)
			}
		}

		for _, payload := range payloadTexts {
			for _, match := range anyRouterNumericUnderscoreRE.FindAllStringSubmatch(payload, -1) {
				if len(match) >= 2 {
					appendID(anyRouterParseInt(match[1]))
				}
			}
			for _, match := range anyRouterNumericKeywordRE.FindAllStringSubmatch(strings.ToLower(payload), -1) {
				if len(match) >= 2 {
					appendID(anyRouterParseInt(match[1]))
				}
			}
		}

		for _, payload := range payloadBuffers {
			for _, value := range anyRouterExtractGobFieldInts(payload, "id") {
				appendID(value)
			}
		}
	}

	return ids
}

func anyRouterExtractGobFieldInts(payload []byte, fieldName string) []int {
	marker := append(append([]byte(fieldName), 0x03), append([]byte("int"), 0x04)...)
	values := make([]int, 0)
	seen := make(map[int]struct{})

	for start := 0; start < len(payload); {
		position := bytes.Index(payload[start:], marker)
		if position < 0 {
			break
		}
		position += start
		if position+len(marker)+1 >= len(payload) {
			break
		}
		encodedLength := int(payload[position+len(marker)])
		delimiter := payload[position+len(marker)+1]
		start = position + len(marker)
		if delimiter != 0x00 {
			continue
		}
		byteLength := encodedLength - 1
		valueStart := position + len(marker) + 2
		valueEnd := valueStart + byteLength
		if byteLength <= 0 || valueEnd > len(payload) {
			continue
		}
		if value := anyRouterDecodeGobSignedInt(payload[valueStart:valueEnd]); value > 0 {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
	}

	return values
}

func anyRouterDecodeGobSignedInt(encoded []byte) int {
	if len(encoded) == 0 || len(encoded) > 8 {
		return 0
	}

	var unsigned uint64
	if encoded[0] < 0x80 {
		unsigned = uint64(encoded[0])
	} else {
		width := int(0x100 - uint16(encoded[0]))
		if width <= 0 || len(encoded) != width+1 || width > 8 {
			return 0
		}
		for i := 1; i < len(encoded); i++ {
			unsigned = (unsigned << 8) | uint64(encoded[i])
		}
	}

	var signed int64
	if unsigned&1 == 0 {
		signed = int64(unsigned >> 1)
	} else {
		signed = -int64((unsigned >> 1) + 1)
	}
	if signed <= 0 || signed > 10_000_000 {
		return 0
	}
	return int(signed)
}
