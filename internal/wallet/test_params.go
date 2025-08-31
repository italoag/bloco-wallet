//go:build !production
// +build !production

package wallet

import "github.com/ethereum/go-ethereum/accounts/keystore"

// Test-optimized scrypt parameters for development
// These parameters are much faster but less secure - only for testing!
const (
	// Fast test parameters (about 10x faster than standard)
	TestScryptN = keystore.LightScryptN    // 4096 instead of 262144
	TestScryptP = keystore.LightScryptP    // 1 instead of 1
	
	// Medium test parameters (about 4x faster than standard)
	TestScryptNMedium = 65536  // 1/4 of standard
	TestScryptPMedium = 1      // Same as standard
	
	// Standard parameters for comparison tests only
	TestScryptNStandard = keystore.StandardScryptN  // 262144
	TestScryptPStandard = keystore.StandardScryptP  // 1
)

// GetTestScryptParams returns appropriate scrypt parameters based on test type
func GetTestScryptParams(testType string) (n, p int) {
	switch testType {
	case "fast":
		return TestScryptN, TestScryptP
	case "medium":
		return TestScryptNMedium, TestScryptPMedium
	case "standard":
		return TestScryptNStandard, TestScryptPStandard
	default:
		// Default to fast for development
		return TestScryptN, TestScryptP
	}
}

// GetTestKeystoreParams returns test-optimized keystore parameters
func GetTestKeystoreParams() (n, p int) {
	return GetTestScryptParams("fast")
}