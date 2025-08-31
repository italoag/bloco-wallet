package wallet

// TestKeystores contains various keystore formats for testing Universal KDF compatibility
var TestKeystores = map[string]string{
	"geth_standard": `{
		"address": "3cc7dc4096856c6e8fa5a179ff6acf7cdbb72772",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "6087dab2f9fdbbfaddc31a909735c1e6"
			},
			"ciphertext": "5318b4d5bcd28de64ee5559e671353e16f075ecae9f99c7a79a38af5f869aa46",
			"kdf": "scrypt",
			"kdfparams": {
				"dklen": 32,
				"n": 262144,
				"p": 1,
				"r": 8,
				"salt": "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097"
			},
			"mac": "517ead924a9d0dc3124507e3393d175ce3ff7c1e96529c6c555ce9e51205e9b2"
		},
		"id": "3198bc9c-6672-5ab3-d995-4942343ae5b6",
		"version": 3
	}`,

	"metamask_variant": `{
		"address": "b5d85cbf7cb3ee0d56b3bb207d5fc4b82f43f511",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "a1b2c3d4e5f6789012345678901234567"
			},
			"ciphertext": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			"kdf": "Scrypt",
			"kdfparams": {
				"N": "262144",
				"R": 8.0,
				"P": 1,
				"dkLen": 32,
				"Salt": "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
			},
			"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		},
		"id": "a1b2c3d4-e5f6-7890-1234-567890abcdef",
		"version": 3
	}`,

	"trust_wallet_mobile": `{
		"address": "d8da6bf26964af9d7eed9e03e53415d37aa96045",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "1234567890abcdef1234567890abcdef"
			},
			"ciphertext": "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
			"kdf": "SCRYPT",
			"kdfparams": {
				"cost": 32768,
				"blocksize": 8,
				"parallel": 1,
				"keylen": 32,
				"SALT": "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
			},
			"mac": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		},
		"id": "12345678-90ab-cdef-1234-567890abcdef",
		"version": 3
	}`,

	"pbkdf2_ledger": `{
		"address": "f39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "abcdef0123456789abcdef0123456789"
			},
			"ciphertext": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"kdf": "pbkdf2",
			"kdfparams": {
				"c": 262144,
				"dklen": 32,
				"prf": "hmac-sha256",
				"salt": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			},
			"mac": "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
		},
		"id": "fedcba98-7654-3210-fedc-ba9876543210",
		"version": 3
	}`,

	"pbkdf2_sha512": `{
		"address": "70997970c51812dc3a010c7d01b50e0d17dc79c8",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "0123456789abcdef0123456789abcdef"
			},
			"ciphertext": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			"kdf": "PBKDF2",
			"kdfparams": {
				"iterations": 500000,
				"length": 32,
				"hash": "hmac-sha512",
				"salt": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
			},
			"mac": "0987654321fedcba0987654321fedcba0987654321fedcba0987654321fedcba"
		},
		"id": "0987654321-fedc-ba09-8765-4321fedcba09",
		"version": 3
	}`,

	"mixed_types_json": `{
		"address": "3c44cdddb6a900fa2b585dd299e03d12fa4293bc",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "fedcba0987654321fedcba0987654321"
			},
			"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"kdf": "scrypt",
			"kdfparams": {
				"n": "131072",
				"r": 8,
				"p": 1.0,
				"dklen": "32",
				"salt": "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"
			},
			"mac": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
		},
		"id": "mixed-types-test-keystore-uuid",
		"version": 3
	}`,
}

// TestPasswords contains the passwords for the test keystores
var TestPasswords = map[string]string{
	"geth_standard":       "testpassword123",
	"metamask_variant":    "testpassword123",
	"trust_wallet_mobile": "testpassword123",
	"pbkdf2_ledger":       "testpassword123",
	"pbkdf2_sha512":       "testpassword123",
	"mixed_types_json":    "testpassword123",
}
