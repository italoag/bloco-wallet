package keepass

import (
	"bytes"
	"errors"
	"testing"

	kp "github.com/tobischo/gokeepasslib/v3"
)

func TestPatchedDecoderDependency(t *testing.T) {
	data := []byte{3, 0xd9, 0xa2, 0x9a, 0x67, 0xfb, 0x4b, 0xb5, 0, 0, 4, 0, 3, 0, 0, 0, 0}
	db := kp.NewDatabase()
	db.Credentials = kp.NewPasswordCredentials("synthetic-dependency-test")
	if err := kp.NewDecoderWithLimits(bytes.NewReader(data), kp.DecodeLimits{}).Decode(db); err == nil {
		t.Fatal("malformed KDBX accepted")
	}
	if err := kp.NewDecoderWithLimits(bytes.NewReader(make([]byte, 9)), kp.DecodeLimits{MaxFileBytes: 8}).Decode(db); !errors.Is(err, kp.ErrDecodeLimitExceeded) {
		t.Fatalf("missing patched decode budget: %v", err)
	}
}
