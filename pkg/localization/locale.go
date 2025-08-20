package localization

import (
	"blocowallet/pkg/config"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var (
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
)

// InitLocalization initializes the i18n bundle and localizer
func InitLocalization(cfg *config.Config) error {
	// Create a new bundle with English as the default language
	bundle = i18n.NewBundle(language.English)

	// Register the TOML unmarshal function
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// Ensure the locale directory exists
	if err := ensureLocaleDir(cfg.LocaleDir); err != nil {
		return err
	}

	// Load all message files from the locale directory
	if err := loadMessageFiles(cfg.LocaleDir); err != nil {
		return err
	}

	// Create a localizer with the configured language
	localizer = i18n.NewLocalizer(bundle, cfg.Language)

	// Populate the Labels map for backward compatibility
	if err := populateLabelsMap(); err != nil {
		return err
	}

	return nil
}

// ensureLocaleDir ensures that the locale directory exists and contains at least the default language file
func ensureLocaleDir(localeDir string) error {
	// Create the locale directory if it doesn't exist
	if _, err := os.Stat(localeDir); os.IsNotExist(err) {
		if err := os.MkdirAll(localeDir, 0755); err != nil {
			return fmt.Errorf("failed to create locale directory: %w", err)
		}
	}

	// Create default language files if they don't exist
	if err := createDefaultLanguageFiles(localeDir); err != nil {
		return fmt.Errorf("failed to create default language files: %w", err)
	}

	return nil
}

// loadMessageFiles loads all message files from the locale directory
func loadMessageFiles(localeDir string) error {
	// Get all .toml files in the locale directory
	files, err := filepath.Glob(filepath.Join(localeDir, "*.toml"))
	if err != nil {
		return fmt.Errorf("failed to list locale files: %w", err)
	}

	// Load each file into the bundle
	for _, file := range files {
		if _, err := bundle.LoadMessageFile(file); err != nil {
			return fmt.Errorf("failed to load message file %s: %w", file, err)
		}
	}

	return nil
}

// createDefaultLanguageFiles creates default language files if they don't exist
func createDefaultLanguageFiles(localeDir string) error {
	// Define the languages to create default files for
	languages := []string{"en", "pt", "es"}

	for _, lang := range languages {
		filename := fmt.Sprintf("language.%s.toml", lang)
		filePath := filepath.Join(localeDir, filename)

		// Skip if the file already exists
		if _, err := os.Stat(filePath); err == nil {
			continue
		}

		// Create the default language file
		if err := createLanguageFile(filePath, lang); err != nil {
			return fmt.Errorf("failed to create language file %s: %w", filePath, err)
		}
	}

	return nil
}

// createLanguageFile creates a language file with default translations
func createLanguageFile(filePath, lang string) error {
	var content string
	var messages map[string]string

	// Start with base messages
	switch lang {
	case "en":
		messages = getEnglishMessages()
		// Adicionar mensagens de criptografia
		for k, v := range DefaultCryptoMessages() {
			messages[k] = v
		}
		// Adicionar mensagens de redes
		for k, v := range DefaultNetworkMessagesEnglish() {
			messages[k] = v
		}
	case "pt":
		messages = getPortugueseMessages()
		// Adicionar mensagens de criptografia
		for k, v := range DefaultCryptoMessagesPortuguese() {
			messages[k] = v
		}
		// Adicionar mensagens de redes
		for k, v := range DefaultNetworkMessagesPortuguese() {
			messages[k] = v
		}
	case "es":
		messages = getSpanishMessages()
		// Adicionar mensagens de criptografia
		for k, v := range DefaultCryptoMessagesSpanish() {
			messages[k] = v
		}
		// Adicionar mensagens de redes
		for k, v := range DefaultNetworkMessagesSpanish() {
			messages[k] = v
		}
	default:
		messages = getEnglishMessages()
		// Adicionar mensagens de criptografia padrão (inglês)
		for k, v := range DefaultCryptoMessages() {
			messages[k] = v
		}
		// Adicionar mensagens de redes (padrão inglês)
		for k, v := range DefaultNetworkMessagesEnglish() {
			messages[k] = v
		}
	}

	// Convert the messages to TOML format
	content = "# " + getLanguageName(lang) + " translations\n\n"
	for key, value := range messages {
		content += "[" + key + "]\n"
		// Use triple-quoted string for multi-line support or escape newlines
		if strings.Contains(value, "\n") {
			// Triple quotes for multi-line strings in TOML
			content += "other = \"\"\"" + value + "\"\"\"\n\n"
		} else {
			// Regular quoted string for single-line values
			content += "other = \"" + value + "\"\n\n"
		}
	}

	// Write the content to the file
	return os.WriteFile(filePath, []byte(content), 0644)
}

// getLanguageName returns the full name of a language based on its code
func getLanguageName(code string) string {
	switch code {
	case "en":
		return "English"
	case "pt":
		return "Portuguese"
	case "es":
		return "Spanish"
	default:
		return "Unknown"
	}
}

// getEnglishMessages returns the default English messages
func getEnglishMessages() map[string]string {
	return map[string]string{
		"welcome_message":            "Welcome to the BLOCO Wallet!\n\nSelect an option from the menu.",
		"mnemonic_phrase":            "Mnemonic Phrase (Keep it Safe!):",
		"enter_password":             "Enter a password to encrypt the wallet:",
		"press_enter":                "Press Enter to continue.",
		"import_wallet_title":        "Import an existing Wallet",
		"wallet_list_instructions":   "Use the arrow keys to navigate, Enter to view details, 'd' to delete a wallet, 'esc' to return to the menu.",
		"status_bar_instructions":    "View: {{.View}} | Press 'esc' to return | 'q' to quit",
		"wallet_list_status_bar":     "View: {{.View}} | Press 'd' to delete | 'esc' to return | 'q' to quit",
		"enter_wallet_password":      "Enter the wallet password:",
		"select_wallet_prompt":       "Select a wallet and enter the password to view the details.",
		"wallet_details_title":       "Wallet Details",
		"ethereum_address":           "Ethereum Address:",
		"public_key":                 "Public Key:",
		"private_key":                "Private Key:",
		"mnemonic_phrase_label":      "Mnemonic Phrase:",
		"press_esc":                  "Press ESC to return to the wallet list.",
		"main_menu_title":            "Main Menu",
		"create_new_wallet":          "Create New",
		"create_new_wallet_desc":     "Generate a new Ethereum wallet",
		"import_wallet":              "Import Wallet",
		"import_wallet_desc":         "Import an existing wallet",
		"configuration":              "Configuration",
		"configuration_desc":         "Configure application settings",
		"networks":                   "Networks",
		"networks_desc":              "Configure blockchain networks",
		"language":                   "Language",
		"language_desc":              "Change application language",
		"import_method_title":        "Select Import Method",
		"import_mnemonic":            "Mnemonic Phrase",
		"import_mnemonic_desc":       "Import using 12-word mnemonic phrase",
		"import_private_key":         "Private Key",
		"import_private_key_desc":    "Import using a private key",
		"import_keystore":            "Keystore File",
		"import_keystore_desc":       "Import using a KeyStoreV3 file",
		"keystore_title":             "Import Wallet via Keystore File",
		"enter_keystore_path":        "Enter the path to the KeyStoreV3 file:",
		"invalid_keystore":           "Invalid keystore file. Please check the path and try again.",
		"invalid_keystore_password":  "Invalid password for the keystore file. Please try again.",
		"back_to_menu":               "Main Menu",
		"back_to_menu_desc":          "Return to the main menu",
		"private_key_title":          "Import Wallet via Private Key",
		"enter_private_key":          "Enter the private key (with or without 0x prefix):",
		"enter_mnemonic":             "Enter your 12-word mnemonic phrase:",
		"invalid_mnemonic":           "Invalid mnemonic phrase. It must be 12 words separated by spaces.",
		"wallet_created":             "Wallet created successfully!",
		"wallet_imported":            "Wallet imported successfully!",
		"wallet_deleted":             "Wallet deleted successfully.",
		"delete_wallet_confirmation": "Are you sure you want to delete this wallet?",
		"yes":                        "Yes",
		"no":                         "No",
		"loading":                    "Loading...",
		"error":                      "Error: {{.Error}}",
		"wallets":                    "Wallets",
		"quit_app":                   "Quit Application",
		"quit_app_desc":              "Exit the program",
		"wallet_action_title":        "Wallet Actions",
		"view_wallet":                "View Details",
		"delete_wallet":              "Delete Wallet",
		"confirm_title":              "Confirm",
		"cancel":                     "Cancel",
		"list_wallets":               "My Wallets",
		"list_wallets_desc":          "View and manage your wallets",
		"exit":                       "Exit",
		"exit_desc":                  "Exit the application",
		"created_at":                 "Created",
		"wallet_type":                "Type",
		"imported_private_key":       "Private Key",
		"imported_mnemonic":          "Mnemonic",
		"imported_keystore":          "Keystore",
		"version":                    "0.2.0",
		"current":                    "Current",
	}
}

// getPortugueseMessages returns the default Portuguese messages
func getPortugueseMessages() map[string]string {
	return map[string]string{
		"welcome_message":            "Bem-vindo ao Gerenciador de Carteiras BLOCO!\n\nSelecione uma opção do menu.",
		"mnemonic_phrase":            "Frase Mnemônica (Mantenha-a Segura!):",
		"enter_password":             "Digite uma senha para criptografar a carteira:",
		"press_enter":                "Pressione Enter para continuar.",
		"import_wallet_title":        "Importar uma Carteira existente",
		"wallet_list_instructions":   "Use as setas para navegar, Enter para ver detalhes, 'd' para deletar uma carteira, 'esc' para voltar ao menu.",
		"status_bar_instructions":    "Visualização: {{.View}} | Pressione 'esc' para voltar | 'q' para sair",
		"wallet_list_status_bar":     "Visualização: {{.View}} | Pressione 'd' para deletar | 'esc' para voltar | 'q' para sair",
		"enter_wallet_password":      "Digite a senha da carteira:",
		"select_wallet_prompt":       "Selecione uma carteira e digite a senha para ver os detalhes.",
		"wallet_details_title":       "Detalhes da Carteira",
		"ethereum_address":           "Endereço Ethereum:",
		"public_key":                 "Chave Pública:",
		"private_key":                "Chave Privada:",
		"mnemonic_phrase_label":      "Frase Mnemônica:",
		"press_esc":                  "Pressione ESC para voltar à lista de carteiras.",
		"main_menu_title":            "Menu Principal",
		"create_new_wallet":          "Criar Nova",
		"create_new_wallet_desc":     "Gerar uma nova carteira Ethereum",
		"import_wallet":              "Importar Carteira",
		"import_wallet_desc":         "Importar uma carteira existente",
		"configuration":              "Configuração",
		"configuration_desc":         "Configurar ajustes da aplicação",
		"networks":                   "Redes",
		"networks_desc":              "Configurar redes blockchain",
		"language":                   "Idioma",
		"language_desc":              "Alterar idioma da aplicação",
		"import_method_title":        "Selecione o Método de Importação",
		"import_mnemonic":            "Frase Mnemônica",
		"import_mnemonic_desc":       "Importar usando frase de 12 palavras",
		"import_private_key":         "Chave Privada",
		"import_private_key_desc":    "Importar usando uma chave privada",
		"import_keystore":            "Arquivo KeyStore",
		"import_keystore_desc":       "Importar arquivo KeyStore V3",
		"keystore_title":             "Importar Carteira via Arquivo KeyStore",
		"enter_keystore_path":        "Digite o caminho para o arquivo KeyStore V3:",
		"invalid_keystore":           "Arquivo keystore inválido. Verifique o caminho e tente novamente.",
		"invalid_keystore_password":  "Senha inválida para o arquivo keystore. Tente novamente.",
		"back_to_menu":               "Voltar",
		"back_to_menu_desc":          "Retornar ao menu principal",
		"private_key_title":          "Importar Carteira via Chave Privada",
		"enter_private_key":          "Digite a chave privada (com ou sem prefixo 0x):",
		"enter_mnemonic":             "Digite sua frase mnemônica de 12 palavras:",
		"invalid_mnemonic":           "Frase mnemônica inválida. Deve ter 12 palavras separadas por espaços.",
		"wallet_created":             "Carteira criada com sucesso!",
		"wallet_imported":            "Carteira importada com sucesso!",
		"wallet_deleted":             "Carteira excluída com sucesso.",
		"delete_wallet_confirmation": "Tem certeza que deseja excluir esta carteira?",
		"yes":                        "Sim",
		"no":                         "Não",
		"loading":                    "Carregando...",
		"error":                      "Erro: {{.Error}}",
		"wallets":                    "Carteiras",
		"quit_app":                   "Sair da Aplicação",
		"quit_app_desc":              "Encerrar o programa",
		"wallet_action_title":        "Ações da Carteira",
		"view_wallet":                "Ver Detalhes",
		"delete_wallet":              "Excluir Carteira",
		"confirm_title":              "Confirmar",
		"cancel":                     "Cancelar",
		"list_wallets":               "Minhas Carteiras",
		"list_wallets_desc":          "Gerenciar as carteiras",
		"exit":                       "Sair",
		"exit_desc":                  "Sair da aplicação",
		"created_at":                 "Criado Em",
		"wallet_type":                "Tipo",
		"imported_private_key":       "Chave Privada",
		"imported_mnemonic":          "Frase Mnemônica",
		"imported_keystore":          "Keystore",
		"version":                    "0.2.0",
		"current":                    "Atual",
	}
}

// getSpanishMessages returns the default Spanish messages
func getSpanishMessages() map[string]string {
	return map[string]string{
		"welcome_message":            "¡Bienvenido al Administrador de Carteras BLOCO!\n\nSeleccione una opción del menú.",
		"mnemonic_phrase":            "Frase Mnemónica (¡Manténgala Segura!):",
		"enter_password":             "Ingrese una contraseña para cifrar la cartera:",
		"press_enter":                "Presione Enter para continuar.",
		"import_wallet_title":        "Importar una Cartera existente",
		"wallet_list_instructions":   "Use las flechas para navegar, Enter para ver detalles, 'd' para eliminar una cartera, 'esc' para volver al menú.",
		"status_bar_instructions":    "Vista: {{.View}} | Presione 'esc' para volver | 'q' para salir",
		"wallet_list_status_bar":     "Vista: {{.View}} | Presione 'd' para eliminar | 'esc' para volver | 'q' para salir",
		"enter_wallet_password":      "Ingrese la contraseña de la cartera:",
		"select_wallet_prompt":       "Seleccione una cartera e ingrese la contraseña para ver los detalles.",
		"wallet_details_title":       "Detalles de la Cartera",
		"ethereum_address":           "Dirección de Ethereum:",
		"public_key":                 "Clave Pública:",
		"private_key":                "Clave Privada:",
		"mnemonic_phrase_label":      "Frase Mnemónica:",
		"press_esc":                  "Presione ESC para volver a la lista de carteras.",
		"main_menu_title":            "Menú Principal",
		"create_new_wallet":          "Crear Nueva",
		"create_new_wallet_desc":     "Generar una nueva cartera Ethereum",
		"import_wallet":              "Importar Cartera",
		"import_wallet_desc":         "Importar una cartera existente",
		"configuration":              "Configuración",
		"configuration_desc":         "Configurar ajustes de la aplicación",
		"networks":                   "Redes",
		"networks_desc":              "Configurar redes blockchain",
		"language":                   "Idioma",
		"language_desc":              "Cambiar idioma de la aplicación",
		"import_method_title":        "Seleccione el Método de Importación",
		"import_mnemonic":            "Frase Mnemotécnica",
		"import_mnemonic_desc":       "Importar usando frase de 12 palabras",
		"import_private_key":         "Clave Privada",
		"import_private_key_desc":    "Importar usando una clave privada",
		"import_keystore":            "Archivo Keystore",
		"import_keystore_desc":       "Importar usando un archivo KeyStoreV3",
		"keystore_title":             "Importar Cartera vía Archivo Keystore",
		"enter_keystore_path":        "Ingrese la ruta al archivo KeyStoreV3:",
		"invalid_keystore":           "Archivo keystore inválido. Verifique la ruta e intente nuevamente.",
		"invalid_keystore_password":  "Contraseña inválida para el archivo keystore. Intente nuevamente.",
		"back_to_menu":               "Volver",
		"back_to_menu_desc":          "Regresar al menú principal",
		"private_key_title":          "Importar Cartera vía Clave Privada",
		"enter_private_key":          "Ingrese la clave privada (con o sin prefijo 0x):",
		"enter_mnemonic":             "Ingrese su frase mnemónica de 12 palabras:",
		"invalid_mnemonic":           "Frase mnemotécnica inválida. Debe tener 12 palabras separadas por espacios.",
		"wallet_created":             "¡Cartera creada exitosamente!",
		"wallet_imported":            "¡Cartera importada exitosamente!",
		"wallet_deleted":             "Cartera eliminada exitosamente.",
		"delete_wallet_confirmation": "¿Está seguro de que desea eliminar esta cartera?",
		"yes":                        "Sí",
		"no":                         "No",
		"loading":                    "Cargando...",
		"error":                      "Error: {{.Error}}",
		"wallets":                    "Carteras",
		"quit_app":                   "Salir de la Aplicación",
		"quit_app_desc":              "Cerrar el programa",
		"wallet_action_title":        "Acciones de la Cartera",
		"view_wallet":                "Ver Detalles",
		"delete_wallet":              "Eliminar Cartera",
		"confirm_title":              "Confirmar",
		"cancel":                     "Cancelar",
		"list_wallets":               "Mis Carteras",
		"list_wallets_desc":          "Ver y administrar tus carteras",
		"exit":                       "Salir",
		"exit_desc":                  "Salir de la aplicación",
		"created_at":                 "Creado En",
		"wallet_type":                "Tipo",
		"imported_private_key":       "Clave Privada",
		"imported_mnemonic":          "Frase Mnemónica",
		"imported_keystore":          "Keystore",
		"version":                    "0.2.0",
		"current":                    "Actual",
	}
}

// populateLabelsMap preenche o mapa Labels com os valores traduzidos para compatibilidade retroativa
func populateLabelsMap() error {
	// Inicializa o mapa global se ainda não estiver inicializado
	if Labels == nil {
		Labels = make(map[string]string)
	}

	// Obtém todas as mensagens disponíveis usando o localizer
	messageKeys := getAllMessageKeys()

	// Adiciona cada mensagem ao mapa Labels
	for _, key := range messageKeys {
		localizedString := Get(key)
		Labels[key] = localizedString
	}

	// Add keystore validation messages
	AddKeystoreValidationMessages()
	// Add wallet import messages
	AddWalletImportMessages()

	return nil
}

// getAllMessageKeys retorna todas as chaves de mensagem conhecidas
func getAllMessageKeys() []string {
	// Combine as chaves de todos os idiomas para garantir que todas estejam presentes
	keysMap := make(map[string]bool)

	// Adiciona chaves em inglês
	for key := range getEnglishMessages() {
		keysMap[key] = true
	}

	// Adiciona chaves de criptografia
	for key := range DefaultCryptoMessages() {
		keysMap[key] = true
	}

	// Adiciona chaves de redes
	for key := range DefaultNetworkMessagesEnglish() {
		keysMap[key] = true
	}

	// Converte o mapa em uma lista de chaves
	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}

	return keys
}

// Get retorna a mensagem localizada para a chave especificada
func Get(key string) string {
	if localizer == nil {
		return key // Retorna a própria chave se o localizer não estiver inicializado
	}

	// Obtém a mensagem traduzida
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID: key,
	})

	if err != nil {
		return key // Retorna a própria chave se não houver tradução
	}

	return msg
}

// GetAvailableLanguages returns a list of available languages based on the locale files
func GetAvailableLanguages(localeDir string) []string {
	// Default languages that should always be available
	defaultLanguages := []string{"en", "pt", "es"}
	availableLanguages := make(map[string]bool)

	// Add default languages to the map
	for _, lang := range defaultLanguages {
		availableLanguages[lang] = true
	}

	// Get all .toml files in the locale directory
	files, err := filepath.Glob(filepath.Join(localeDir, "*.toml"))
	if err != nil {
		// If there's an error, return just the default languages
		return defaultLanguages
	}

	// Extract language codes from filenames
	for _, file := range files {
		filename := filepath.Base(file)
		// Expected format: language.{lang}.toml
		parts := strings.Split(filename, ".")
		if len(parts) >= 3 && parts[0] == "language" && parts[2] == "toml" {
			lang := parts[1]
			availableLanguages[lang] = true
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(availableLanguages))
	for lang := range availableLanguages {
		result = append(result, lang)
	}

	return result
}

// GetLanguageName returns the full name of a language based on its code
func GetLanguageName(code string) string {
	switch code {
	case "en":
		return "English"
	case "pt":
		return "Portuguese"
	case "es":
		return "Spanish"
	default:
		return code
	}
}
