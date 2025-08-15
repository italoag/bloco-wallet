package wallet

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"hash"
	"strconv"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

// CryptoParams represents the crypto section parameters for KDF operations
type CryptoParams struct {
	KDF          string                 `json:"kdf"`
	KDFParams    map[string]interface{} `json:"kdfparams"`
	Cipher       string                 `json:"cipher"`
	CipherText   string                 `json:"ciphertext"`
	CipherParams map[string]interface{} `json:"cipherparams"`
	MAC          string                 `json:"mac"`
}

// UniversalKDFService suporta qualquer configuração KDF válida
type UniversalKDFService struct {
	supportedKDFs map[string]KDFHandler
	logger        KDFLogger
}

// KDFHandler interface para diferentes tipos de KDF
type KDFHandler interface {
	DeriveKey(password string, params map[string]interface{}) ([]byte, error)
	ValidateParams(params map[string]interface{}) error
	GetDefaultParams() map[string]interface{}
	GetParamRange(param string) (min, max interface{})
}

// KDFLogger interface para logging de operações KDF
type KDFLogger interface {
	LogKDFAttempt(kdf string, params map[string]interface{})
	LogKDFSuccess(kdf string, duration string)
	LogKDFError(kdf string, err error)
	LogParamValidation(param string, value interface{}, valid bool)
}

// SimpleKDFLogger implementação básica do logger
type SimpleKDFLogger struct{}

func (l *SimpleKDFLogger) LogKDFAttempt(kdf string, params map[string]interface{}) {
	// Removed console output to avoid cluttering the TUI
	// fmt.Printf("🔑 Tentando KDF: %s com parâmetros: %v\n", kdf, params)
}

func (l *SimpleKDFLogger) LogKDFSuccess(kdf string, duration string) {
	// Removed console output to avoid cluttering the TUI
	// fmt.Printf("✅ KDF %s concluído em %s\n", kdf, duration)
}

func (l *SimpleKDFLogger) LogKDFError(kdf string, err error) {
	// Removed console output to avoid cluttering the TUI
	// fmt.Printf("❌ Erro no KDF %s: %v\n", kdf, err)
}

func (l *SimpleKDFLogger) LogParamValidation(param string, value interface{}, valid bool) {
	// Removed console output to avoid cluttering the TUI
	// status := "✅"
	// if !valid {
	// 	status = "❌"
	// }
	// fmt.Printf("%s Parâmetro %s = %v (válido: %t)\n", status, param, value, valid)
}

// NewUniversalKDFService cria serviço universal de KDF
func NewUniversalKDFService() *UniversalKDFService {
	service := &UniversalKDFService{
		supportedKDFs: make(map[string]KDFHandler),
		logger:        &SimpleKDFLogger{},
	}

	// Registra handlers padrão
	service.RegisterKDF("scrypt", &ScryptHandler{})
	service.RegisterKDF("pbkdf2", &PBKDF2Handler{})
	service.RegisterKDF("pbkdf2-sha256", &PBKDF2Handler{hashFunc: sha256.New})
	service.RegisterKDF("pbkdf2-sha512", &PBKDF2Handler{hashFunc: sha512.New})

	return service
}

// RegisterKDF registra um novo handler de KDF
func (uks *UniversalKDFService) RegisterKDF(name string, handler KDFHandler) {
	uks.supportedKDFs[name] = handler
}

// DeriveKey deriva chave usando qualquer KDF suportado
func (uks *UniversalKDFService) DeriveKey(password string, crypto *CryptoParams) ([]byte, error) {
	kdfName := crypto.KDF

	// Normaliza nome do KDF (case-insensitive, variações)
	normalizedKDF := uks.normalizeKDFName(kdfName)

	handler, exists := uks.supportedKDFs[normalizedKDF]
	if !exists {
		return nil, fmt.Errorf("KDF não suportado: %s (normalizado: %s)", kdfName, normalizedKDF)
	}

	// Log da tentativa
	uks.logger.LogKDFAttempt(normalizedKDF, crypto.KDFParams)

	// Valida parâmetros antes de usar
	if err := handler.ValidateParams(crypto.KDFParams); err != nil {
		uks.logger.LogKDFError(normalizedKDF, err)
		return nil, fmt.Errorf("parâmetros KDF inválidos: %w", err)
	}

	// Deriva a chave
	start := getCurrentTime()
	derivedKey, err := handler.DeriveKey(password, crypto.KDFParams)
	duration := getElapsedTime(start)

	if err != nil {
		uks.logger.LogKDFError(normalizedKDF, err)
		return nil, err
	}

	uks.logger.LogKDFSuccess(normalizedKDF, duration)
	return derivedKey, nil
}

// normalizeKDFName normaliza nomes de KDF para diferentes variações
func (uks *UniversalKDFService) normalizeKDFName(kdf string) string {
	kdfMap := map[string]string{
		"scrypt":        "scrypt",
		"Scrypt":        "scrypt",
		"SCRYPT":        "scrypt",
		"pbkdf2":        "pbkdf2",
		"PBKDF2":        "pbkdf2",
		"pbkdf2-sha256": "pbkdf2-sha256",
		"pbkdf2-sha512": "pbkdf2-sha512",
		"pbkdf2_sha256": "pbkdf2-sha256",
		"pbkdf2_sha512": "pbkdf2-sha512",
	}

	if normalized, exists := kdfMap[kdf]; exists {
		return normalized
	}

	return kdf // Retorna original se não encontrou mapeamento
}

// ScryptHandler implementa KDF Scrypt com máxima compatibilidade
type ScryptHandler struct{}

func (sh *ScryptHandler) DeriveKey(password string, params map[string]interface{}) ([]byte, error) {
	// Extrai parâmetros com fallbacks seguros
	n := sh.getIntParam(params, []string{"n", "N", "cost"}, 262144)
	r := sh.getIntParam(params, []string{"r", "R", "blocksize"}, 8)
	p := sh.getIntParam(params, []string{"p", "P", "parallel"}, 1)
	dklen := sh.getIntParam(params, []string{"dklen", "dkLen", "keylen", "length"}, 32)

	// Extrai salt
	salt, err := sh.getSaltParam(params)
	if err != nil {
		return nil, err
	}

	return scrypt.Key([]byte(password), salt, n, r, p, dklen)
}

func (sh *ScryptHandler) ValidateParams(params map[string]interface{}) error {
	// Valida N (deve ser potência de 2)
	n := sh.getIntParam(params, []string{"n", "N", "cost"}, 262144)
	if n < 1024 {
		return fmt.Errorf("parâmetro N muito baixo: %d (mínimo: 1024)", n)
	}
	if n > 67108864 { // 2^26
		return fmt.Errorf("parâmetro N muito alto: %d (máximo: 67108864)", n)
	}
	if !sh.isPowerOfTwo(n) {
		return fmt.Errorf("parâmetro N deve ser potência de 2: %d", n)
	}

	// Valida R
	r := sh.getIntParam(params, []string{"r", "R", "blocksize"}, 8)
	if r < 1 || r > 1024 {
		return fmt.Errorf("parâmetro R inválido: %d (range: 1-1024)", r)
	}

	// Valida P
	p := sh.getIntParam(params, []string{"p", "P", "parallel"}, 1)
	if p < 1 || p > 16 {
		return fmt.Errorf("parâmetro P inválido: %d (range: 1-16)", p)
	}

	// Valida dklen
	dklen := sh.getIntParam(params, []string{"dklen", "dkLen", "keylen"}, 32)
	if dklen < 16 || dklen > 128 {
		return fmt.Errorf("parâmetro dklen inválido: %d (range: 16-128)", dklen)
	}

	// Verifica se salt existe
	if _, err := sh.getSaltParam(params); err != nil {
		return fmt.Errorf("salt inválido: %w", err)
	}

	// Calcula uso de memória e valida
	memoryUsage := int64(128 * n * r)
	if memoryUsage > 2*1024*1024*1024 { // 2GB limit
		return fmt.Errorf("uso de memória muito alto: %d bytes (máximo: 2GB)", memoryUsage)
	}

	return nil
}

func (sh *ScryptHandler) GetDefaultParams() map[string]interface{} {
	return map[string]interface{}{
		"n":     262144,
		"r":     8,
		"p":     1,
		"dklen": 32,
	}
}

func (sh *ScryptHandler) GetParamRange(param string) (min, max interface{}) {
	ranges := map[string][2]int{
		"n":     {1024, 67108864},
		"r":     {1, 1024},
		"p":     {1, 16},
		"dklen": {16, 128},
	}

	if r, exists := ranges[param]; exists {
		return r[0], r[1]
	}
	return nil, nil
}

// getIntParam extrai parâmetro inteiro com múltiplos nomes possíveis
func (sh *ScryptHandler) getIntParam(params map[string]interface{}, names []string, defaultValue int) int {
	for _, name := range names {
		if value, exists := params[name]; exists {
			return sh.convertToInt(value, defaultValue)
		}
	}
	return defaultValue
}

// convertToInt converte valor para int lidando com diferentes tipos JSON
func (sh *ScryptHandler) convertToInt(value interface{}, defaultValue int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	}
	return defaultValue
}

// getSaltParam extrai salt de diferentes formatos
func (sh *ScryptHandler) getSaltParam(params map[string]interface{}) ([]byte, error) {
	saltNames := []string{"salt", "Salt", "SALT"}

	for _, name := range saltNames {
		if value, exists := params[name]; exists {
			return sh.convertToBytes(value)
		}
	}

	return nil, fmt.Errorf("salt não encontrado")
}

// convertToBytes converte valor para []byte
func (sh *ScryptHandler) convertToBytes(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case string:
		// Tenta hex decode primeiro
		if len(v)%2 == 0 {
			if bytes, err := hexToBytes(v); err == nil {
				return bytes, nil
			}
		}
		// Fallback para string direta
		return []byte(v), nil
	case []byte:
		return v, nil
	case []interface{}:
		// Array de números
		bytes := make([]byte, len(v))
		for i, item := range v {
			if num, ok := item.(float64); ok {
				bytes[i] = byte(num)
			} else {
				return nil, fmt.Errorf("item do array salt inválido: %v", item)
			}
		}
		return bytes, nil
	default:
		return nil, fmt.Errorf("tipo de salt não suportado: %T", value)
	}
}

// isPowerOfTwo verifica se número é potência de 2
func (sh *ScryptHandler) isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// PBKDF2Handler implementa KDF PBKDF2 com suporte a diferentes hash functions
type PBKDF2Handler struct {
	hashFunc func() hash.Hash
}

func (ph *PBKDF2Handler) DeriveKey(password string, params map[string]interface{}) ([]byte, error) {
	// Extrai parâmetros
	iterations := ph.getIntParam(params, []string{"c", "iter", "iterations", "rounds"}, 262144)
	dklen := ph.getIntParam(params, []string{"dklen", "dkLen", "keylen", "length"}, 32)

	// Extrai salt
	salt, err := ph.getSaltParam(params)
	if err != nil {
		return nil, err
	}

	// Determina função hash
	hashFunc := ph.getHashFunction(params)

	return pbkdf2.Key([]byte(password), salt, iterations, dklen, hashFunc), nil
}

func (ph *PBKDF2Handler) ValidateParams(params map[string]interface{}) error {
	// Valida iterations
	iterations := ph.getIntParam(params, []string{"c", "iter", "iterations"}, 262144)
	if iterations < 1000 {
		return fmt.Errorf("iterações muito baixas: %d (mínimo: 1000)", iterations)
	}
	if iterations > 10000000 {
		return fmt.Errorf("iterações muito altas: %d (máximo: 10000000)", iterations)
	}

	// Valida dklen
	dklen := ph.getIntParam(params, []string{"dklen", "dkLen"}, 32)
	if dklen < 16 || dklen > 128 {
		return fmt.Errorf("dklen inválido: %d (range: 16-128)", dklen)
	}

	// Verifica salt
	if _, err := ph.getSaltParam(params); err != nil {
		return fmt.Errorf("salt inválido: %w", err)
	}

	return nil
}

func (ph *PBKDF2Handler) GetDefaultParams() map[string]interface{} {
	return map[string]interface{}{
		"c":     262144,
		"dklen": 32,
		"prf":   "hmac-sha256",
	}
}

func (ph *PBKDF2Handler) GetParamRange(param string) (min, max interface{}) {
	ranges := map[string][2]int{
		"c":     {1000, 10000000},
		"dklen": {16, 128},
	}

	if r, exists := ranges[param]; exists {
		return r[0], r[1]
	}
	return nil, nil
}

// getHashFunction determina função hash baseada em parâmetros
func (ph *PBKDF2Handler) getHashFunction(params map[string]interface{}) func() hash.Hash {
	if ph.hashFunc != nil {
		return ph.hashFunc
	}

	// Verifica parâmetro prf (Pseudo Random Function)
	prfNames := []string{"prf", "PRF", "hash", "hashFunc"}
	for _, name := range prfNames {
		if value, exists := params[name]; exists {
			if str, ok := value.(string); ok {
				switch str {
				case "hmac-sha256", "sha256", "SHA256":
					return sha256.New
				case "hmac-sha512", "sha512", "SHA512":
					return sha512.New
				}
			}
		}
	}

	// Default para SHA256
	return sha256.New
}

// Métodos auxiliares reutilizados
func (ph *PBKDF2Handler) getIntParam(params map[string]interface{}, names []string, defaultValue int) int {
	for _, name := range names {
		if value, exists := params[name]; exists {
			return ph.convertToInt(value, defaultValue)
		}
	}
	return defaultValue
}

func (ph *PBKDF2Handler) convertToInt(value interface{}, defaultValue int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	}
	return defaultValue
}

func (ph *PBKDF2Handler) getSaltParam(params map[string]interface{}) ([]byte, error) {
	saltNames := []string{"salt", "Salt", "SALT"}

	for _, name := range saltNames {
		if value, exists := params[name]; exists {
			return ph.convertToBytes(value)
		}
	}

	return nil, fmt.Errorf("salt não encontrado")
}

func (ph *PBKDF2Handler) convertToBytes(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case string:
		if len(v)%2 == 0 {
			if bytes, err := hexToBytes(v); err == nil {
				return bytes, nil
			}
		}
		return []byte(v), nil
	case []byte:
		return v, nil
	case []interface{}:
		bytes := make([]byte, len(v))
		for i, item := range v {
			if num, ok := item.(float64); ok {
				bytes[i] = byte(num)
			} else {
				return nil, fmt.Errorf("item do array salt inválido: %v", item)
			}
		}
		return bytes, nil
	default:
		return nil, fmt.Errorf("tipo de salt não suportado: %T", value)
	}
}

// KDFCompatibilityAnalyzer analisa compatibilidade de KeyStores
type KDFCompatibilityAnalyzer struct {
	service *UniversalKDFService
}

func NewKDFCompatibilityAnalyzer() *KDFCompatibilityAnalyzer {
	return &KDFCompatibilityAnalyzer{
		service: NewUniversalKDFService(),
	}
}

// AnalyzeKeyStoreCompatibility analisa se um KeyStore é compatível
func (kca *KDFCompatibilityAnalyzer) AnalyzeKeyStoreCompatibility(keystoreData map[string]interface{}) *CompatibilityReport {
	report := &CompatibilityReport{
		Issues:      make([]string, 0),
		Warnings:    make([]string, 0),
		Suggestions: make([]string, 0),
		Compatible:  true,
	}

	// Extrai dados crypto
	cryptoData, ok := keystoreData["crypto"].(map[string]interface{})
	if !ok {
		report.Issues = append(report.Issues, "Estrutura 'crypto' não encontrada ou inválida")
		report.Compatible = false
		return report
	}

	// Verifica KDF
	kdfType, ok := cryptoData["kdf"].(string)
	if !ok {
		report.Issues = append(report.Issues, "Tipo KDF não encontrado")
		report.Compatible = false
		return report
	}

	report.KDFType = kdfType

	// Verifica se KDF é suportado
	normalizedKDF := kca.service.normalizeKDFName(kdfType)
	handler, exists := kca.service.supportedKDFs[normalizedKDF]
	if !exists {
		report.Issues = append(report.Issues, fmt.Sprintf("KDF não suportado: %s", kdfType))
		report.Compatible = false
		return report
	}

	report.NormalizedKDF = normalizedKDF

	// Extrai parâmetros KDF
	kdfParams, ok := cryptoData["kdfparams"].(map[string]interface{})
	if !ok {
		report.Issues = append(report.Issues, "Parâmetros KDF não encontrados")
		report.Compatible = false
		return report
	}

	report.Parameters = kdfParams

	// Valida parâmetros
	if err := handler.ValidateParams(kdfParams); err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("Parâmetros inválidos: %v", err))
		report.Compatible = false
	} else {
		report.Warnings = append(report.Warnings, "Parâmetros validados com sucesso")
	}

	// Analisa segurança dos parâmetros
	securityAnalysis := kca.analyzeParameterSecurity(normalizedKDF, kdfParams)
	report.SecurityLevel = securityAnalysis.Level
	report.Suggestions = append(report.Suggestions, securityAnalysis.Suggestions...)

	// Verifica compatibilidade de versão
	if version, ok := keystoreData["version"].(float64); ok {
		if int(version) != 3 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Versão não padrão: %d (esperado: 3)", int(version)))
		}
	}

	return report
}

// CompatibilityReport relatório de compatibilidade
type CompatibilityReport struct {
	Compatible    bool                   `json:"compatible"`
	KDFType       string                 `json:"kdf_type"`
	NormalizedKDF string                 `json:"normalized_kdf"`
	Parameters    map[string]interface{} `json:"parameters"`
	SecurityLevel string                 `json:"security_level"`
	Issues        []string               `json:"issues"`
	Warnings      []string               `json:"warnings"`
	Suggestions   []string               `json:"suggestions"`
}

// SecurityAnalysis análise de segurança dos parâmetros
type SecurityAnalysis struct {
	Level       string   `json:"level"`
	Suggestions []string `json:"suggestions"`
}

// analyzeParameterSecurity analisa segurança dos parâmetros
func (kca *KDFCompatibilityAnalyzer) analyzeParameterSecurity(kdf string, params map[string]interface{}) SecurityAnalysis {
	analysis := SecurityAnalysis{
		Level:       "Medium",
		Suggestions: make([]string, 0),
	}

	if kdf == "scrypt" {
		n := kca.getIntParam(params, "n", 262144)
		r := kca.getIntParam(params, "r", 8)
		p := kca.getIntParam(params, "p", 1)

		// Análise baseada em complexidade computacional
		complexity := float64(n * r * p)

		if complexity < 1000000 { // < 1M operations
			analysis.Level = "Low"
			analysis.Suggestions = append(analysis.Suggestions, "Parâmetros muito baixos para segurança moderna")
		} else if complexity < 10000000 { // < 10M operations
			analysis.Level = "Medium"
			analysis.Suggestions = append(analysis.Suggestions, "Segurança adequada para uso geral")
		} else if complexity < 100000000 { // < 100M operations
			analysis.Level = "High"
			analysis.Suggestions = append(analysis.Suggestions, "Boa segurança para aplicações sensíveis")
		} else {
			analysis.Level = "Very High"
			analysis.Suggestions = append(analysis.Suggestions, "Segurança muito alta, adequada para aplicações críticas")
		}

		// Verifica se são parâmetros padrão
		if n == 262144 && r == 8 && p == 1 {
			analysis.Suggestions = append(analysis.Suggestions, "⚠️ Usando parâmetros padrão - considere personalização para aplicações de alto valor")
		}

	} else if kdf == "pbkdf2" {
		iterations := kca.getIntParam(params, "c", 262144)

		if iterations < 100000 {
			analysis.Level = "Low"
			analysis.Suggestions = append(analysis.Suggestions, "Iterações insuficientes para segurança moderna")
		} else if iterations < 500000 {
			analysis.Level = "Medium"
			analysis.Suggestions = append(analysis.Suggestions, "PBKDF2 menos resistente que scrypt contra ataques GPU")
		} else {
			analysis.Level = "High"
			analysis.Suggestions = append(analysis.Suggestions, "Boas iterações, mas considere migrar para scrypt")
		}
	}

	return analysis
}

func (kca *KDFCompatibilityAnalyzer) getIntParam(params map[string]interface{}, name string, defaultValue int) int {
	if value, exists := params[name]; exists {
		switch v := value.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
	}
	return defaultValue
}

// Funções auxiliares
func hexToBytes(s string) ([]byte, error) {
	// Implementação simples de hex decode
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("string hex deve ter comprimento par")
	}

	bytes := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		high := hexCharToInt(s[i])
		low := hexCharToInt(s[i+1])
		if high == -1 || low == -1 {
			return nil, fmt.Errorf("caractere hex inválido")
		}
		bytes[i/2] = byte(high<<4 | low)
	}
	return bytes, nil
}

func hexCharToInt(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}

func getCurrentTime() time.Time {
	return time.Now()
}

func getElapsedTime(start time.Time) string {
	duration := time.Since(start)
	return duration.String()
}
