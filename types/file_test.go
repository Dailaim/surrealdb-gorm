package types

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// These are pure unit tests — no database required.

func TestNewFileNormalizesKey(t *testing.T) {
	require.Equal(t, "/report.pdf", NewFile("b", "report.pdf").Key, "key gets a leading /")
	require.Equal(t, "/report.pdf", NewFile("b", "/report.pdf").Key, "existing leading / is kept")
	require.Equal(t, "b:/report.pdf", NewFile("b", "report.pdf").String())
}

func TestFileMarshalCBOR_ExactBytes(t *testing.T) {
	// SurrealDB wire format: Tag(55) + array(2) + text "mybucket" + text "/mykey".
	//   d8 37 = tag 55
	//   82    = array of 2
	//   68 …  = text(8) "mybucket"
	//   66 …  = text(6) "/mykey"
	b, err := NewFile("mybucket", "/mykey").MarshalCBOR()
	require.NoError(t, err)
	require.Equal(t,
		"d8378268"+hex.EncodeToString([]byte("mybucket"))+"66"+hex.EncodeToString([]byte("/mykey")),
		hex.EncodeToString(b),
	)

	// And it must decode back through the fxamacker tag machinery.
	var tag cbor.Tag
	require.NoError(t, cbor.Unmarshal(b, &tag))
	require.Equal(t, TagFile, tag.Number)
}

func TestFileCBORRoundTrip(t *testing.T) {
	orig := NewFile("bucket", "/a/b/c.png")
	b, err := orig.MarshalCBOR()
	require.NoError(t, err)

	var got File
	require.NoError(t, got.UnmarshalCBOR(b))
	require.Equal(t, orig, got)
}

func TestFileJSON(t *testing.T) {
	b, err := json.Marshal(NewFile("mybucket", "/mykey"))
	require.NoError(t, err)
	require.JSONEq(t, `"mybucket:/mykey"`, string(b))

	cases := map[string]string{
		`"mybucket:/mykey"`:                             "mybucket:/mykey", // plain string
		`"f\"mybucket:/mykey\""`:                        "mybucket:/mykey", // SurrealQL f"" literal
		`["mybucket","/mykey"]`:                         "mybucket:/mykey", // [bucket, key] array
		`{"bucket":"mybucket","key":"/mykey"}`:          "mybucket:/mykey", // object
		`{"Number":55,"Content":["mybucket","/mykey"]}`: "mybucket:/mykey", // cbor.Tag JSON form
	}
	for input, want := range cases {
		var f File
		require.NoError(t, json.Unmarshal([]byte(input), &f), "input %s", input)
		require.Equal(t, want, f.String(), "input %s", input)
	}
}

func TestFileScan(t *testing.T) {
	cases := []interface{}{
		"mybucket:/mykey",
		[]byte("mybucket:/mykey"),
		File{Bucket: "mybucket", Key: "/mykey"},
		[]interface{}{"mybucket", "/mykey"},
		map[string]interface{}{"bucket": "mybucket", "key": "/mykey"},
	}
	for _, in := range cases {
		var f File
		require.NoError(t, f.Scan(in), "scan %T", in)
		require.Equal(t, "mybucket", f.Bucket, "scan %T", in)
		require.Equal(t, "/mykey", f.Key, "scan %T", in)
	}

	// nil scans to the zero value without error.
	var f File
	require.NoError(t, f.Scan(nil))
	require.Equal(t, File{}, f)
}

func TestFileValue(t *testing.T) {
	v, err := NewFile("b", "/k").Value()
	require.NoError(t, err)
	require.Equal(t, "b:/k", v)
}

func TestFileGormDataType(t *testing.T) {
	require.Equal(t, "file", File{}.GormDataType())
}
