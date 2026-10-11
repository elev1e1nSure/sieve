package configs

import (
	"errors"
	"fmt"
	"strings"
)

// batVariables are the cmd variables Flowseal's strategy scripts set before
// launching winws, mapped to sieve's own placeholders. Keys are lower-case
// because cmd variable names are case-insensitive.
var batVariables = map[string]string{
	"bin":           binPlaceholder + `\`,
	"lists":         listsPlaceholder + `\`,
	"gamefiltertcp": gameTCPPlaceholder,
	"gamefilterudp": gameUDPPlaceholder,
}

// parseBat extracts the winws arguments from one of Flowseal's general*.bat
// strategy scripts. It understands only as much cmd syntax as that single
// command uses — quotes, caret escapes, caret line continuations and a known
// set of variables — and rejects anything else instead of guessing, so an
// upstream format change surfaces as a skipped strategy, not a broken winws
// command line.
func parseBat(name string, data []byte) (Config, error) {
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf") // UTF-8 BOM
	text = strings.ReplaceAll(text, "\r\n", "\n")

	start := winwsCommandStart(text)
	if start < 0 {
		return Config{}, errors.New("no winws.exe command found")
	}

	tokens := splitCommand(text[start:])
	exe := -1
	for i, token := range tokens {
		if strings.HasSuffix(strings.ToLower(token), "winws.exe") {
			exe = i
			break
		}
	}
	if exe < 0 || exe == len(tokens)-1 {
		return Config{}, errors.New("winws.exe command has no arguments")
	}

	args := make([]string, 0, len(tokens)-exe-1)
	for _, token := range tokens[exe+1:] {
		arg, err := expandBatVariables(token)
		if err != nil {
			return Config{}, err
		}
		if !strings.HasPrefix(arg, "--") {
			return Config{}, fmt.Errorf("unexpected token %q in winws command", token)
		}
		args = append(args, arg)
	}

	return Config{Name: name, Args: args}, nil
}

func winwsCommandStart(text string) int {
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.ToLower(strings.TrimSpace(line))
		isComment := strings.HasPrefix(trimmed, "::") || strings.HasPrefix(trimmed, "rem ")
		if !isComment && strings.Contains(trimmed, "winws.exe") {
			return offset
		}
		offset += len(line)
	}
	return -1
}

// splitCommand tokenizes one cmd command the way cmd hands it to the child:
// whitespace separates tokens, double quotes group and are dropped, a caret
// outside quotes makes the next character literal, and a caret before a
// newline continues the command on the next line.
func splitCommand(text string) []string {
	var tokens []string
	var current strings.Builder
	inToken, quoted := false, false
	flush := func() {
		if inToken {
			tokens = append(tokens, current.String())
			current.Reset()
			inToken = false
		}
	}

scan:
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\n':
			break scan
		case quoted:
			if c == '"' {
				quoted = false
			} else {
				current.WriteByte(c)
			}
		case c == '"':
			quoted, inToken = true, true
		case c == '^':
			if i+1 < len(text) {
				i++
				if text[i] != '\n' {
					current.WriteByte(text[i])
					inToken = true
				}
			}
		case c == ' ' || c == '\t':
			flush()
		default:
			current.WriteByte(c)
			inToken = true
		}
	}
	flush()

	return tokens
}

func expandBatVariables(token string) (string, error) {
	var expanded strings.Builder
	rest := token
	for {
		start := strings.IndexByte(rest, '%')
		if start < 0 {
			expanded.WriteString(rest)
			return expanded.String(), nil
		}
		expanded.WriteString(rest[:start])

		name, after, ok := strings.Cut(rest[start+1:], "%")
		if !ok {
			return "", fmt.Errorf("unterminated variable in %q", token)
		}
		if name == "" {
			expanded.WriteByte('%')
		} else if value, known := batVariables[strings.ToLower(name)]; known {
			expanded.WriteString(value)
		} else {
			return "", fmt.Errorf("unknown variable %%%s%% in %q", name, token)
		}
		rest = after
	}
}
