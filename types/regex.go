package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Regex represents a SurrealDB regular expression.
// It stores the raw pattern string and optional flags.
type Regex struct {
	Pattern string
	Flags   string
}

func NewRegex(pattern, flags string) Regex {
	return Regex{Pattern: pattern, Flags: flags}
}

func (Regex) GormDataType() string { return "string" }

func (r Regex) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Regex) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("cannot unmarshal %s into Regex: %w", string(data), err)
	}
	return r.parse(s)
}

func (r *Regex) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		return r.parse(v)
	case []byte:
		return r.parse(string(v))
	default:
		return fmt.Errorf("cannot scan %T into Regex", value)
	}
}

func (r Regex) Value() (driver.Value, error) {
	if r.Pattern == "" {
		return nil, nil
	}
	return r.String(), nil
}

// String returns the SurrealDB regex format: "/pattern/flags".
func (r Regex) String() string {
	if r.Flags != "" {
		return fmt.Sprintf("/%s/%s", r.Pattern, r.Flags)
	}
	return fmt.Sprintf("/%s/", r.Pattern)
}

// Compile returns a Go *regexp.Regexp.
func (r Regex) Compile() (*regexp.Regexp, error) {
	return regexp.Compile(r.Pattern)
}

func (r *Regex) parse(s string) error {
	if !strings.HasPrefix(s, "/") {
		// No leading slash: treat entire string as pattern
		r.Pattern = s
		r.Flags = ""
		return nil
	}
	// Find the last slash after the first one
	lastSlash := strings.LastIndex(s[1:], "/")
	if lastSlash < 0 {
		// Only one slash at start: no real delimiters
		r.Pattern = s[1:]
		r.Flags = ""
		return nil
	}
	// Adjust lastSlash to index in full string
	lastSlash++ // skip the leading slash
	patternPart := s[1:lastSlash]
	flagsPart := s[lastSlash+1:]
	r.Pattern = patternPart
	r.Flags = flagsPart
	return nil
}
