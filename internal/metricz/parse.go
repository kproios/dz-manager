// Package metricz receives MetricZ HTTP exports (#5) and turns them into the
// live map snapshot and server performance series. The receiver is written
// from MetricZ's documented push protocol; metricz-exporter (AGPL) is not used.
package metricz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sample is one Prometheus exposition sample.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Decode accepts either plain Prometheus text or MetricZ's serialized form
// (a JSON array of lines) and returns the text.
func Decode(body []byte) (string, error) {
	b := bytes.TrimSpace(body)
	if len(b) > 0 && b[0] == '[' {
		var lines []string
		if err := json.Unmarshal(b, &lines); err != nil {
			return "", fmt.Errorf("invalid serialized payload: %w", err)
		}
		return strings.Join(lines, "\n"), nil
	}
	return string(b), nil
}

// Parse parses Prometheus text exposition (the subset MetricZ emits).
// Malformed lines are counted, not fatal.
func Parse(text string) (samples []Sample, bad int) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			bad++
			continue
		}
		samples = append(samples, s)
	}
	return samples, bad
}

func parseLine(line string) (Sample, error) {
	var s Sample
	i := strings.IndexAny(line, "{ ")
	if i <= 0 {
		return s, errors.New("no value")
	}
	s.Name = line[:i]
	rest := line[i:]
	if rest[0] == '{' {
		labels, n, err := parseLabels(rest)
		if err != nil {
			return s, err
		}
		s.Labels, rest = labels, rest[n:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, errors.New("no value")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return s, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return s, errors.New("non-finite value")
	}
	s.Value = v
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	return s, nil
}

// parseLabels parses {k="v",...} and returns the map and bytes consumed.
func parseLabels(s string) (map[string]string, int, error) {
	out := map[string]string{}
	i := 1
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			return nil, 0, errors.New("unterminated labels")
		}
		if s[i] == '}' {
			return out, i + 1, nil
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq <= 0 || i+eq+1 >= len(s) || s[i+eq+1] != '"' {
			return nil, 0, errors.New("bad label")
		}
		key := strings.TrimSpace(s[i : i+eq])
		i += eq + 2
		var val strings.Builder
		for {
			if i >= len(s) {
				return nil, 0, errors.New("unterminated label value")
			}
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				switch s[i+1] {
				case 'n':
					val.WriteByte('\n')
				default:
					val.WriteByte(s[i+1])
				}
				i += 2
				continue
			}
			if c == '"' {
				i++
				break
			}
			val.WriteByte(c)
			i++
		}
		out[key] = val.String()
	}
}
