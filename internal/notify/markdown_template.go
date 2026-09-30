package notify

import "strings"

// Markdown does not honor backslash escapes inside code. Render code variables
// literally and widen their delimiters so values cannot close the code region.
// Other values use normal Markdown escaping. Validation runs before this helper.
func renderMarkdownTemplate(source string, values map[string]string) string {
	escaped := make(map[string]string, len(values))
	for key, value := range values {
		escaped[key] = escapeMarkdown(value)
	}
	var out strings.Builder
	start := 0
	for index := 0; index < len(source); {
		marker := source[index]
		if marker != '`' && marker != '~' {
			index++
			continue
		}
		count := markdownDelimiterRun(source, index, marker)
		fenced := count >= 3 && markdownFencePrefix(source, index)
		backslashes := 0
		for previous := index - 1; previous >= 0 && source[previous] == '\\'; previous-- {
			backslashes++
		}
		if !fenced && (marker == '~' || backslashes%2 != 0) {
			index += count
			continue
		}
		contentStart := index + count
		if fenced {
			newline := strings.IndexByte(source[contentStart:], '\n')
			if newline < 0 {
				index += count
				continue
			}
			contentStart += newline + 1
		}
		closing, closingCount := -1, 0
		for next := contentStart; next < len(source); {
			offset := strings.IndexByte(source[next:], marker)
			if offset < 0 {
				break
			}
			next += offset
			run := markdownDelimiterRun(source, next, marker)
			if fenced && run >= count && markdownFencePrefix(source, next) {
				end := strings.IndexByte(source[next+run:], '\n')
				if end < 0 {
					end = len(source) - next - run
				}
				if strings.TrimSpace(source[next+run:next+run+end]) == "" {
					closing, closingCount = next, run
					break
				}
			} else if !fenced && run == count {
				closing, closingCount = next, run
				break
			}
			next += run
		}
		if closing < 0 {
			if !fenced {
				index += count
				continue
			}
			closing = len(source) // CommonMark allows an unclosed fence through EOF.
		}
		contentEnd := closing
		if fenced && closing < len(source) {
			contentEnd = strings.LastIndexByte(source[:closing], '\n') + 1
		}
		literal := source[contentStart:contentEnd]
		if !fenced && strings.HasPrefix(literal, " ") && strings.HasSuffix(literal, " ") && strings.TrimSpace(literal) != "" {
			literal = literal[1 : len(literal)-1]
		}
		literal, _ = renderTemplate(literal, values)
		literal = strings.ReplaceAll(strings.ReplaceAll(literal, "\x00", ""), "\r\n", "\n")
		if !fenced {
			literal = strings.ReplaceAll(literal, "\n", " ")
		}
		width := count
		for offset := 0; offset < len(literal); {
			if literal[offset] == marker {
				run := markdownDelimiterRun(literal, offset, marker)
				if run >= width {
					width = run + 1
				}
				offset += run
			} else {
				offset++
			}
		}
		normal, _ := renderTemplate(source[start:index], escaped)
		out.WriteString(normal)
		delimiter := strings.Repeat(string(marker), width)
		out.WriteString(delimiter)
		if fenced {
			info, _ := renderTemplate(source[index+count:contentStart], escaped)
			out.WriteString(info)
			out.WriteString(literal)
			if !strings.HasSuffix(literal, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(source[contentEnd:closing])
		} else if strings.Trim(literal, " ") != "" {
			out.WriteString(" " + literal + " ")
		} else {
			out.WriteString(literal)
		}
		out.WriteString(delimiter)
		index = closing + closingCount
		start = index
	}
	normal, _ := renderTemplate(source[start:], escaped)
	out.WriteString(normal)
	return out.String()
}

func markdownDelimiterRun(source string, index int, marker byte) int {
	end := index
	for end < len(source) && source[end] == marker {
		end++
	}
	return end - index
}

func markdownFencePrefix(source string, index int) bool {
	start := strings.LastIndexByte(source[:index], '\n') + 1
	prefix := source[start:index]
	return len(prefix) <= 3 && strings.Trim(prefix, " ") == ""
}
