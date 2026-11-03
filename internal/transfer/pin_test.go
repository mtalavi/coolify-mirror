package transfer

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"testing"
)

func TestPinBeginningWithPlus(t *testing.T) {
	want := bytes.Repeat([]byte{0xfb}, 32)
	pin := base64.StdEncoding.EncodeToString(want)
	for _, fragment := range []string{"pin=" + pin, "pin=" + url.QueryEscape(pin)} {
		query, err := url.ParseQuery(fragment)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodePin(query.Get("pin"))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("leading plus lost after fragment parsing: %v", err)
		}
	}
}
