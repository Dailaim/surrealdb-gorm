package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// TagFile is SurrealDB's CBOR tag number for the file type (SurrealDB v3;
// requires the server's experimental `files` feature). The tagged value is a
// two-element array [bucket, key] of text strings.
const TagFile uint64 = 55

// File is a SurrealDB v3 file pointer, written in SurrealQL as f"bucket:/key".
//
// The server must be started with the experimental files feature enabled
// (e.g. `surreal start --allow-experimental files`). Over the wire it is
// encoded as CBOR tag 55 with content [bucket, key].
type File struct {
	Bucket string
	Key    string
}

// NewFile builds a File. The key is normalized to start with "/".
func NewFile(bucket, key string) File {
	return File{Bucket: bucket, Key: normalizeFileKey(key)}
}

func (File) GormDataType() string { return "file" }

// String renders the "bucket:/key" form (without the SurrealQL f"" wrapper).
func (f File) String() string {
	return f.Bucket + ":" + normalizeFileKey(f.Key)
}

// MarshalCBOR emits Tag(55, [bucket, key]) — the SurrealDB wire format.
func (f File) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(cbor.Tag{
		Number:  TagFile,
		Content: []interface{}{f.Bucket, normalizeFileKey(f.Key)},
	})
}

// UnmarshalCBOR parses Tag(55, [bucket, key]).
func (f *File) UnmarshalCBOR(data []byte) error {
	var tag cbor.Tag
	if err := cbor.Unmarshal(data, &tag); err != nil {
		return err
	}
	return f.fromArray(tag.Content)
}

// fromArray fills the File from a decoded [bucket, key] array (elements may be
// string or []byte depending on the decoder).
func (f *File) fromArray(content interface{}) error {
	arr, ok := content.([]interface{})
	if !ok || len(arr) != 2 {
		return fmt.Errorf("invalid file value: expected [bucket, key], got %#v", content)
	}
	f.Bucket = toStr(arr[0])
	f.Key = toStr(arr[1])
	return nil
}

// MarshalJSON renders the "bucket:/key" string form for APIs / the JSON pipeline.
func (f File) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

// UnmarshalJSON accepts the shapes the read pipeline may produce: a plain string
// ("bucket:/key" or the SurrealQL f"bucket:/key" literal), a [bucket, key]
// array, or a {bucket, key} / cbor.Tag-style {Number, Content} object.
func (f *File) UnmarshalJSON(data []byte) error {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 || string(data) == "null" {
		return nil
	}

	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return f.parseString(s)
	case '[':
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		if len(arr) != 2 {
			return fmt.Errorf("invalid file array: %s", string(data))
		}
		f.Bucket, f.Key = arr[0], arr[1]
		return nil
	case '{':
		var obj map[string]interface{}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		// {bucket, key}
		if b, ok := obj["bucket"]; ok {
			f.Bucket = toStr(b)
			f.Key = toStr(obj["key"])
			return nil
		}
		// cbor.Tag JSON form {Number, Content:[bucket,key]}
		if c, ok := obj["Content"]; ok {
			return f.fromArray(c)
		}
		return fmt.Errorf("unrecognized file object: %s", string(data))
	}
	return fmt.Errorf("cannot unmarshal %s into File", string(data))
}

// parseString parses "bucket:/key" or the f"bucket:/key" literal form.
func (f *File) parseString(s string) error {
	s = strings.TrimSpace(s)
	// Strip an optional f"..." wrapper.
	if strings.HasPrefix(s, "f\"") && strings.HasSuffix(s, "\"") {
		s = s[2 : len(s)-1]
	}
	idx := strings.Index(s, ":")
	if idx < 0 {
		return fmt.Errorf("invalid file string %q: expected bucket:/key", s)
	}
	f.Bucket = s[:idx]
	f.Key = s[idx+1:]
	return nil
}

// Scan implements sql.Scanner.
func (f *File) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		return f.parseString(v)
	case []byte:
		return f.parseString(string(v))
	case File:
		*f = v
		return nil
	case *File:
		if v != nil {
			*f = *v
		}
		return nil
	case []interface{}:
		return f.fromArray(v)
	case map[string]interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return f.UnmarshalJSON(b)
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("cannot scan %T into File", value)
		}
		return f.UnmarshalJSON(b)
	}
}

// Value implements driver.Valuer, returning the "bucket:/key" string form.
func (f File) Value() (driver.Value, error) {
	return f.String(), nil
}

func normalizeFileKey(k string) string {
	if k == "" || strings.HasPrefix(k, "/") {
		return k
	}
	return "/" + k
}

func toStr(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}
