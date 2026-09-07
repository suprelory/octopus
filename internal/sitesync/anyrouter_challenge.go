package sitesync

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	anyRouterArg1Pattern           = regexp.MustCompile(`var\s+arg1\s*=\s*['"]([0-9a-fA-F]+)['"]`)
	anyRouterMappingPattern        = regexp.MustCompile(`for\(var m=\[([^\]]+)\],p=L\(0x115\)`)
	anyRouterEncodedArrayPattern   = regexp.MustCompile(`var\s+N=\[(.*?)\];a0i=`)
	anyRouterArrayStringPattern    = regexp.MustCompile(`'([^']*)'`)
	anyRouterRotationTargetPattern = regexp.MustCompile(`\}\(a0i,\s*(0x[0-9a-fA-F]+|\d+)\)`)
	anyRouterBaseOffsetPattern     = regexp.MustCompile(`d=d-(0x[0-9a-fA-F]+|\d+);`)
)

func anyRouterSolveAcwScV2(html string) string {
	arg1Match := anyRouterArg1Pattern.FindStringSubmatch(html)
	if len(arg1Match) < 2 {
		return ""
	}
	arg1 := strings.ToUpper(strings.TrimSpace(arg1Match[1]))
	if arg1 == "" {
		return ""
	}

	mappingMatch := anyRouterMappingPattern.FindStringSubmatch(html)
	if len(mappingMatch) < 2 {
		return ""
	}
	mappingParts := strings.Split(mappingMatch[1], ",")
	mapping := make([]int, 0, len(mappingParts))
	for _, part := range mappingParts {
		value, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(part))
		if !ok {
			return ""
		}
		mapping = append(mapping, value)
	}

	arrayMatch := anyRouterEncodedArrayPattern.FindStringSubmatch(html)
	if len(arrayMatch) < 2 {
		return ""
	}
	rawMatches := anyRouterArrayStringPattern.FindAllStringSubmatch(arrayMatch[1], -1)
	if len(rawMatches) == 0 {
		return ""
	}
	encodedItems := make([]string, 0, len(rawMatches))
	for _, match := range rawMatches {
		if len(match) >= 2 {
			encodedItems = append(encodedItems, match[1])
		}
	}

	target := 0x760bf
	if targetMatch := anyRouterRotationTargetPattern.FindStringSubmatch(html); len(targetMatch) >= 2 {
		if parsed, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(targetMatch[1])); ok {
			target = parsed
		}
	}

	baseOffset := 0xfb
	if baseMatch := anyRouterBaseOffsetPattern.FindStringSubmatch(html); len(baseMatch) >= 2 {
		if parsed, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(baseMatch[1])); ok {
			baseOffset = parsed
		}
	}

	xorSeed := anyRouterResolveChallengeXorSeed(encodedItems, baseOffset, target)
	if xorSeed == "" {
		return ""
	}

	reordered := make([]byte, len(mapping))
	for index, ch := range arg1 {
		for mappedIndex, value := range mapping {
			if value == index+1 {
				reordered[mappedIndex] = byte(ch)
			}
		}
	}

	var builder strings.Builder
	for i := 0; i+1 < len(reordered) && i+1 < len(xorSeed); i += 2 {
		left, err := strconv.ParseUint(string(reordered[i:i+2]), 16, 8)
		if err != nil {
			return ""
		}
		right, err := strconv.ParseUint(xorSeed[i:i+2], 16, 8)
		if err != nil {
			return ""
		}
		builder.WriteString(fmt.Sprintf("%02x", byte(left)^byte(right)))
	}
	return builder.String()
}

func anyRouterResolveChallengeXorSeed(encodedItems []string, baseOffset int, target int) string {
	if len(encodedItems) == 0 {
		return ""
	}
	decodeAt := func(rotation int, index int) string {
		offset := index - baseOffset
		if offset < 0 || len(encodedItems) == 0 {
			return ""
		}
		raw := encodedItems[(offset+rotation)%len(encodedItems)]
		decoded, ok := anyRouterDecodeObfuscatedBase64String(raw)
		if !ok {
			return ""
		}
		return decoded
	}

	evaluate := func(rotation int) (int, bool) {
		parseAt := func(index int) (int, bool) {
			return anyRouterJSParseIntPrefix(decodeAt(rotation, index))
		}

		a, ok := parseAt(0x117)
		if !ok {
			return 0, false
		}
		b, ok := parseAt(0x111)
		if !ok {
			return 0, false
		}
		c, ok := parseAt(0xfb)
		if !ok {
			return 0, false
		}
		d, ok := parseAt(0x10e)
		if !ok {
			return 0, false
		}
		e, ok := parseAt(0x101)
		if !ok {
			return 0, false
		}
		f, ok := parseAt(0xfd)
		if !ok {
			return 0, false
		}
		g, ok := parseAt(0x102)
		if !ok {
			return 0, false
		}
		h, ok := parseAt(0x122)
		if !ok {
			return 0, false
		}
		i, ok := parseAt(0x112)
		if !ok {
			return 0, false
		}
		j, ok := parseAt(0x11d)
		if !ok {
			return 0, false
		}
		k, ok := parseAt(0x11c)
		if !ok {
			return 0, false
		}
		l, ok := parseAt(0x114)
		if !ok {
			return 0, false
		}

		value := -a/1*(b/2) + -c/3*(d/4) + -e/5*(-f/6) + -g/7*(h/8) + i/9 + j/10*(k/11) + l/12
		return value, true
	}

	for rotation := 0; rotation < len(encodedItems); rotation++ {
		value, ok := evaluate(rotation)
		if !ok || value != target {
			continue
		}
		seed := decodeAt(rotation, 0x115)
		if seed != "" {
			return strings.TrimSpace(seed)
		}
	}
	return ""
}

func anyRouterJSParseIntPrefix(value string) (int, bool) {
	trimmed := strings.TrimLeftFunc(strings.TrimSpace(value), unicode.IsSpace)
	if trimmed == "" {
		return 0, false
	}

	sign := 1
	switch trimmed[0] {
	case '+':
		trimmed = trimmed[1:]
	case '-':
		sign = -1
		trimmed = trimmed[1:]
	}

	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, false
	}

	number, err := strconv.Atoi(trimmed[:digits])
	if err != nil {
		return 0, false
	}
	return sign * number, true
}

func anyRouterParseIntegerLiteral(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		base = 16
		value = value[2:]
	}
	parsed, err := strconv.ParseInt(value, base, 64)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func anyRouterDecodeBase64String(value string) (string, bool) {
	buf, ok := anyRouterDecodeBase64Buffer(value)
	if !ok {
		return "", false
	}
	return string(buf), true
}

func anyRouterDecodeObfuscatedBase64String(value string) (string, bool) {
	translated, ok := anyRouterTranslateObfuscatedBase64(value)
	if !ok {
		return "", false
	}
	buf, ok := anyRouterDecodeBase64Buffer(translated)
	if !ok {
		return "", false
	}
	return string(buf), true
}

func anyRouterTranslateObfuscatedBase64(value string) (string, bool) {
	const obfuscatedAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/="
	const standardAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="

	var builder strings.Builder
	builder.Grow(len(value))
	for _, ch := range value {
		index := strings.IndexRune(obfuscatedAlphabet, ch)
		if index < 0 {
			return "", false
		}
		builder.WriteByte(standardAlphabet[index])
	}
	return builder.String(), true
}

func anyRouterDecodeBase64Buffer(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}

	candidates := []string{
		value,
		value + strings.Repeat("=", (4-len(value)%4)%4),
		strings.ReplaceAll(strings.ReplaceAll(value, "-", "+"), "_", "/"),
	}
	candidates = append(candidates, candidates[2]+strings.Repeat("=", (4-len(candidates[2])%4)%4))

	for _, candidate := range candidates {
		for _, encoding := range []*base64.Encoding{
			base64.StdEncoding,
			base64.RawStdEncoding,
			base64.URLEncoding,
			base64.RawURLEncoding,
		} {
			buf, err := encoding.DecodeString(candidate)
			if err == nil {
				return buf, true
			}
		}
	}
	return nil, false
}
