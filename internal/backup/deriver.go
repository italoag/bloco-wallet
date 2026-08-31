package backup

import (
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2idDeriver derives archive keys with Argon2id. Parameters are
// production-strength by default.
type Argon2idDeriver struct {
	Time    uint32
	Memory  uint32
	Threads uint8
}

// NewArgon2idDeriver creates the default-strength deriver.
func NewArgon2idDeriver() *Argon2idDeriver {
	return &Argon2idDeriver{Time: 3, Memory: 64 * 1024, Threads: 4}
}

// DeriveKey implements KeyDeriver.
func (deriver *Argon2idDeriver) DeriveKey(password []byte, salt []byte) ([]byte, error) {
	return deriver.DeriveKeyWithParams(password, salt, 0, 0, 0)
}

// DeriveKeyWithParams derives with explicit parameters; zero values fall
// back to the deriver defaults so older archives stay openable.
func (deriver *Argon2idDeriver) DeriveKeyWithParams(password []byte, salt []byte, timeCost, memory uint32, threads uint8) ([]byte, error) {
	if deriver == nil {
		return nil, fmt.Errorf("backup: nil deriver")
	}
	if len(salt) != KDFSaltBytes {
		return nil, fmt.Errorf("backup: invalid salt size")
	}
	if timeCost == 0 {
		timeCost = deriver.Time
	}
	if timeCost == 0 {
		timeCost = 3
	}
	if memory == 0 {
		memory = deriver.Memory
	}
	if memory == 0 {
		memory = 64 * 1024
	}
	if threads == 0 {
		threads = deriver.Threads
	}
	if threads == 0 {
		threads = 4
	}
	if timeCost > 16 || memory > 512*1024 || threads > 16 {
		return nil, fmt.Errorf("backup: kdf parameters outside policy")
	}
	key := argon2.IDKey(password, salt, timeCost, memory, threads, 32)
	if len(key) != 32 {
		return nil, fmt.Errorf("backup: key derivation failed")
	}
	return key, nil
}
