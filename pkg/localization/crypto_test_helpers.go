package localization

// InitCryptoMessagesForTesting inicializa as mensagens de criptografia para testes unitários
// sem depender de arquivos de idioma ou do Viper. Esta função também limpa qualquer
// estado global prévio para evitar contaminação entre testes (e.g., idioma anterior
// e chaves residuais no mapa Labels).
func InitCryptoMessagesForTesting() {
	// Força idioma padrão dos testes para inglês para previsibilidade
	SetCurrentLanguage("en")

	// Reinicializa o mapa global para evitar vazamento de chaves entre testes
	Labels = make(map[string]string)

	// Adiciona todas as mensagens de criptografia/base em inglês para uso nos testes
	for key, value := range DefaultCryptoMessages() {
		Labels[key] = value
	}
}

// GetForTesting para testes retorna a mensagem diretamente do mapa Labels
// Esta função é uma versão simplificada da função Get para uso em testes
func GetForTesting(key string) string {
	if Labels == nil {
		return key
	}

	if value, ok := Labels[key]; ok {
		return value
	}
	return key
}
