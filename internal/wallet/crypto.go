package wallet

import (
	"blocowallet/pkg/localization"
	"crypto/subtle"
)

// SecureCompare realiza comparação em tempo constante de duas strings
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// PasswordValidationError representa um erro de validação de senha
type PasswordValidationError struct {
	TooShort         bool
	NoLowercase      bool
	NoUppercase      bool
	NoDigitOrSpecial bool
}

// Error implementa a interface de erro
func (e PasswordValidationError) Error() string {
	if !e.HasErrors() {
		return ""
	}
	return localization.Get("password_validation_failed")
}

// HasErrors verifica se há algum erro de validação
func (e PasswordValidationError) HasErrors() bool {
	return e.TooShort || e.NoLowercase || e.NoUppercase || e.NoDigitOrSpecial
}

// GetErrorMessage retorna uma mensagem de erro específica para o primeiro erro encontrado
func (e PasswordValidationError) GetErrorMessage() string {
	if e.TooShort {
		return localization.Get("password_too_short")
	}
	if e.NoLowercase {
		return localization.Get("password_no_lowercase")
	}
	if e.NoUppercase {
		return localization.Get("password_no_uppercase")
	}
	if e.NoDigitOrSpecial {
		return localization.Get("password_no_digit_or_special")
	}
	return ""
}

// ValidatePassword valida a complexidade da senha
func ValidatePassword(password string) (PasswordValidationError, bool) {
	var err PasswordValidationError

	// Verificar tamanho mínimo
	if len(password) < 15 {
		err.TooShort = true
		return err, false
	}

	// Verificar presença de letra minúscula
	hasLower := false
	// Verificar presença de letra maiúscula
	hasUpper := false
	// Verificar presença de dígito ou caractere especial
	hasDigitOrSpecial := false

	for _, c := range password {
		switch {
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		default:
			hasDigitOrSpecial = true
		}
	}

	err.NoLowercase = !hasLower
	err.NoUppercase = !hasUpper
	err.NoDigitOrSpecial = !hasDigitOrSpecial

	return err, !err.HasErrors()
}
