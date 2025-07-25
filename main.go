package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"blocowallet/config"
	"blocowallet/infrastructure"
	"blocowallet/interfaces"
	"blocowallet/localization"
	"blocowallet/usecases"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/go-errors/errors"
)

const (
	logFileName        = "blocowallet.log"
	logFilePermissions = 0666
)

func main() {
	// Configuração de logging
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePermissions)
	if err != nil {
		fmt.Printf("Erro ao abrir o arquivo de log: %v\n", err)
		os.Exit(1)
	}
	defer closeFile(logFile)
	log.SetOutput(logFile)

	// Obter o diretório home do usuário
	homeDir, err := os.UserHomeDir()
	if err != nil {
		handleError("Erro ao obter o diretório home do usuário", err)
	}

	appDir := filepath.Join(homeDir, ".wallets")

	// Garantir que o diretório base exista
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		err := os.MkdirAll(appDir, os.ModePerm)
		if err != nil {
			handleError("Erro ao criar o diretório da aplicação", err)
		}
	}

	// Inicializar a configuração
	cfg, err := config.LoadConfig(appDir)
	if err != nil {
		handleError("Erro ao carregar a configuração", err)
	}

	// Inicializar a localização
	err = localization.SetLanguage(cfg.Language, appDir)
	if err != nil {
		handleError("Erro ao carregar os arquivos de localização", err)
	}

	// Garantir que o diretório de wallets exista
	if _, err := os.Stat(cfg.WalletsDir); os.IsNotExist(err) {
		err := os.MkdirAll(cfg.WalletsDir, os.ModePerm)
		if err != nil {
			handleError("Erro ao criar o diretório de wallets", err)
		}
	}

	// Inicializar o repositório
	repo, err := infrastructure.NewSQLiteRepository(cfg.DatabasePath)
	if err != nil {
		handleError("Erro ao inicializar o banco de dados", err)
	}
	defer closeResource(repo)

	// Inicializar o keystore
	ks := keystore.NewKeyStore(cfg.WalletsDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Usar o serviço no modelo CLI
	service := usecases.NewWalletService(repo, ks)
	model := interfaces.NewCLIModel(service)

	// Iniciar o programa Bubble Tea com tela cheia
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		handleError("Erro ao executar o programa", err)
	}
}

// Funções auxiliares

func handleError(message string, err error) {
	log.Println(errors.Wrap(err, 0).ErrorStack())
	fmt.Println(message)
	os.Exit(1)
}

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		log.Printf("Erro ao fechar o arquivo: %v\n", err)
	}
}

func closeResource(repo *infrastructure.SQLiteRepository) {
	if err := repo.Close(); err != nil {
		log.Printf("Erro ao fechar o banco de dados: %v\n", err)
	}
}
