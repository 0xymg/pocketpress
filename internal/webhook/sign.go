package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Sign computes a webhook's X-PocketPress-Signature header value for a request
// body sent at the given unix timestamp:
//
//	sha256=hex(HMAC-SHA256(secret, timestamp + "." + raw_body))
//
// The timestamp is rendered as its base-10 unix seconds and joined to the raw
// body with a single "." — exactly the string a receiver reconstructs from the
// X-PocketPress-Timestamp header and the bytes it received. Minting a fresh
// timestamp per attempt (the worker does) means each attempt carries its own
// signature.
func Sign(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
