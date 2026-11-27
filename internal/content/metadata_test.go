package content

import (
	"strings"
	"testing"
)

func TestNormalizeMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "empty", raw: "", want: "{}"},
		{name: "whitespace", raw: "  \n", want: "{}"},
		{name: "empty object", raw: "{}", want: "{}"},
		{name: "simple", raw: `{"hero":"launch","priority":1}`, want: `{"hero":"launch","priority":1}`},
		{name: "rejects array", raw: `["a"]`, wantErr: "Metadata must be a JSON object."},
		{name: "rejects string", raw: `"x"`, wantErr: "Metadata must be a JSON object."},
		{name: "rejects null", raw: `null`, wantErr: "Metadata must be a JSON object."},
		{name: "rejects invalid", raw: `{`, wantErr: "Metadata must be valid JSON."},
		// json.Valid rejects multi-value input before the decoder More() check.
		{name: "rejects trailing", raw: `{}{}`, wantErr: "Metadata must be valid JSON."},
		{name: "rejects oversize", raw: `{"k":"` + strings.Repeat("x", metadataMaxBytes) + `"}`, wantErr: "Metadata must be at most"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, errMsg := NormalizeMetadata([]byte(tc.raw))
			if tc.wantErr != "" {
				if errMsg == "" || !strings.Contains(errMsg, tc.wantErr) {
					t.Fatalf("NormalizeMetadata(%q) err = %q, want containing %q", tc.raw, errMsg, tc.wantErr)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("NormalizeMetadata(%q) unexpected err %q", tc.raw, errMsg)
			}
			if got != tc.want {
				t.Fatalf("NormalizeMetadata(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	// Too many top-level keys.
	keys := make([]string, 0, metadataMaxKeys+1)
	for i := 0; i < metadataMaxKeys+1; i++ {
		keys = append(keys, `"k`+itoa(i)+`":1`)
	}
	raw := "{" + strings.Join(keys, ",") + "}"
	if _, errMsg := NormalizeMetadata([]byte(raw)); !strings.Contains(errMsg, "top-level keys") {
		t.Fatalf("too many keys err = %q", errMsg)
	}

	// Nesting too deep: {a:{a:{...}}} past metadataMaxDepth.
	deep := "{}"
	for i := 0; i < metadataMaxDepth; i++ {
		deep = `{"a":` + deep + `}`
	}
	if _, errMsg := NormalizeMetadata([]byte(deep)); !strings.Contains(errMsg, "nesting depth") {
		t.Fatalf("deep nesting err = %q for %s", errMsg, deep)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
