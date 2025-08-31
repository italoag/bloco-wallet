//go:build production
// +build production

package wallet

import "github.com/ethereum/go-ethereum/accounts/keystore"

// Production scrypt parameters - secure but slower
const (
	// Use standard secure parameters in production
	TestScryptN = keystore.StandardScryptN  // 262144
	TestScryptP = keystore.StandardScryptP  // 1
	
	// Medium parameters are same as standard in production
	TestScryptNMedium = keystore.StandardScryptN
	TestScryptPMedium = keystore.StandardScryptP
	
	// Standard parameters
	TestScryptNStandard = keystore.StandardScryptN
	TestScryptPStandard = keystore.StandardScryptP
)

// GetTestScryptParams returns secure scrypt parameters for production
func GetTestScryptParams(testType string) (n, p int) {
	// In production, always use secure parameters regardless of test type
	return keystore.StandardScryptN, keystore.StandardScryptP
}

// GetTestKeystoreParams returns production-grade keystore parameters
func GetTestKeystoreParams() (n, p int) {
	return keystore.StandardScryptN, keystore.StandardScryptP
}