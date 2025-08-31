# Design Document

## Overview

Este documento descreve o design para corrigir os problemas de persistência e classificação de redes no BlocoWallet. O foco é garantir que as configurações sejam carregadas corretamente usando Viper e que a classificação de redes como "custom" seja baseada na existência da rede na chainlist.

## Architecture

### Current Architecture Issues
- Uso de diretório hardcoded `~/.wallets` em vez de usar configuração do Viper
- Classificação incorreta de todas as redes como "custom"
- Falta de integração com chainlist para validação de redes
- Inconsistência no carregamento de configurações após compilação

### Proposed Architecture
- Uso consistente do Viper para carregamento de configurações
- Integração com ChainListService para validação de redes
- Sistema de classificação inteligente de redes (standard vs custom)
- Carregamento de configuração centralizado e consistente

## Components and Interfaces

### 1. Enhanced Configuration Manager

```go
// ConfigurationManager gerencia o carregamento e salvamento de configurações
type ConfigurationManager struct {
    viper       *viper.Viper
    chainList   *blockchain.ChainListService
    configPath  string
}

// NewConfigurationManager cria uma nova instância do gerenciador
func NewConfigurationManager(chainList *blockchain.ChainListService) *ConfigurationManager

// LoadConfiguration carrega a configuração usando Viper
func (cm *ConfigurationManager) LoadConfiguration() (*config.Config, error)

// SaveConfiguration salva a configuração mantendo compatibilidade com Viper
func (cm *ConfigurationManager) SaveConfiguration(cfg *config.Config) error

// GetConfigPath retorna o caminho do arquivo de configuração
func (cm *ConfigurationManager) GetConfigPath() string
```

### 2. Network Classification Service

```go
// NetworkClassificationService classifica redes como standard ou custom
type NetworkClassificationService struct {
    chainList *blockchain.ChainListService
}

// NewNetworkClassificationService cria uma nova instância do serviço
func NewNetworkClassificationService(chainList *blockchain.ChainListService) *NetworkClassificationService

// ClassifyNetwork determina se uma rede é standard ou custom
func (ncs *NetworkClassificationService) ClassifyNetwork(chainID int64, name string) (NetworkType, error)

// ValidateNetworkAgainstChainList valida uma rede contra a chainlist
func (ncs *NetworkClassificationService) ValidateNetworkAgainstChainList(chainID int64) (*blockchain.ChainInfo, error)

// GenerateNetworkKey gera uma chave apropriada baseada na classificação
func (ncs *NetworkClassificationService) GenerateNetworkKey(network config.Network, networkType NetworkType) string
```

### 3. Enhanced Network Manager

```go
// NetworkManager gerencia operações de rede com classificação inteligente
type NetworkManager struct {
    configManager       *ConfigurationManager
    classificationService *NetworkClassificationService
    chainList          *blockchain.ChainListService
}

// NewNetworkManager cria uma nova instância do gerenciador de redes
func NewNetworkManager(configManager *ConfigurationManager, chainList *blockchain.ChainListService) *NetworkManager

// AddNetwork adiciona uma nova rede com classificação automática
func (nm *NetworkManager) AddNetwork(network config.Network) error

// UpdateNetwork atualiza uma rede existente
func (nm *NetworkManager) UpdateNetwork(key string, network config.Network) error

// RemoveNetwork remove uma rede
func (nm *NetworkManager) RemoveNetwork(key string) error

// LoadNetworks carrega todas as redes da configuração
func (nm *NetworkManager) LoadNetworks() (map[string]config.Network, error)
```

## Data Models

### Network Type Classification
```go
type NetworkType int

const (
    NetworkTypeStandard NetworkType = iota // Rede existe na chainlist
    NetworkTypeCustom                      // Rede não existe na chainlist
)

func (nt NetworkType) String() string {
    switch nt {
    case NetworkTypeStandard:
        return "standard"
    case NetworkTypeCustom:
        return "custom"
    default:
        return "unknown"
    }
}
```

### Enhanced Network Configuration
```go
type EnhancedNetwork struct {
    config.Network
    Type        NetworkType `json:"type"`
    IsValidated bool        `json:"is_validated"`
    Source      string      `json:"source"` // "chainlist" or "manual"
}
```

### Network Key Format
- **Standard Networks**: `{sanitized_name}_{chain_id}` (sem prefixo "custom")
- **Custom Networks**: `custom_{sanitized_name}_{chain_id}`
- **Caracteres permitidos**: letras, números e underscore
- **Sanitização**: caracteres inválidos substituídos por underscore

## Error Handling

### Error Classification
1. **Configuration Errors**: Erros de carregamento/salvamento de configuração
2. **ChainList Errors**: Erros de acesso à API da chainlist
3. **Network Validation Errors**: Erros de validação de redes
4. **File System Errors**: Erros de acesso a arquivos

### Error Recovery Strategies
- **ChainList Offline**: Permitir adicionar redes como custom com aviso
- **Configuration Corruption**: Backup automático e recuperação
- **Network Validation Failure**: Fallback para classificação manual
- **File Access Errors**: Mensagens de erro específicas e sugestões de correção

## Testing Strategy

### Unit Tests
- **ConfigurationManager Tests**
  - Testes de carregamento com Viper
  - Testes de salvamento mantendo compatibilidade
  - Testes de tratamento de erros

- **NetworkClassificationService Tests**
  - Testes de classificação de redes conhecidas
  - Testes de classificação de redes custom
  - Testes de validação contra chainlist
  - Testes de geração de chaves

- **NetworkManager Tests**
  - Testes de adição de redes com classificação automática
  - Testes de atualização e remoção
  - Testes de carregamento de redes

### Integration Tests
- **End-to-End Configuration Tests**
  - Testes de persistência após reinicialização
  - Testes de carregamento com diferentes configurações
  - Testes de migração de configurações existentes

## Implementation Approach

### Phase 1: Configuration Management Refactoring
1. Implementar ConfigurationManager com uso correto do Viper
2. Refatorar funções de UI para usar o novo gerenciador
3. Garantir carregamento consistente de configurações

### Phase 2: Network Classification System
1. Implementar NetworkClassificationService
2. Integrar com ChainListService para validação
3. Implementar geração inteligente de chaves

### Phase 3: Enhanced Network Management
1. Implementar NetworkManager com classificação automática
2. Atualizar UI para mostrar tipo de rede
3. Implementar validação automática contra chainlist

### Phase 4: UI Integration and Testing
1. Atualizar componentes de UI para usar novos serviços
2. Implementar indicadores visuais para tipos de rede
3. Adicionar testes abrangentes

## Security Considerations

### Configuration Security
- Validação de caminhos de arquivo para prevenir path traversal
- Sanitização de entrada para chaves de rede
- Backup seguro de configurações

### Network Validation Security
- Validação de URLs de RPC para prevenir SSRF
- Timeout apropriado para chamadas de API
- Tratamento seguro de dados da chainlist

## Performance Considerations

### ChainList Integration
- Cache de dados da chainlist para reduzir chamadas de API
- Timeout configurável para chamadas de rede
- Fallback para operação offline

### Configuration Loading
- Carregamento lazy de configurações quando necessário
- Cache de configurações em memória
- Operações de I/O otimizadas

## Backward Compatibility

### Configuration Migration
- Suporte para configurações existentes sem classificação
- Migração automática de redes para novo formato
- Preservação de configurações personalizadas

### API Compatibility
- Manter compatibilidade com interfaces existentes
- Adicionar novos métodos sem quebrar funcionalidade existente
- Documentar mudanças para desenvolvedores

## User Experience Improvements

### Visual Indicators
- Ícones diferentes para redes standard vs custom
- Tooltips explicando a origem da rede
- Status de validação visível na interface

### Error Messages
- Mensagens específicas para diferentes tipos de erro
- Sugestões de correção para problemas comuns
- Indicação clara quando chainlist não está disponível

## Configuration File Structure

### Enhanced TOML Format
```toml
[networks]

[networks.ethereum_1]
name = "Ethereum Mainnet"
rpc_endpoint = "https://eth.llamarpc.com"
chain_id = 1
symbol = "ETH"
explorer = "https://etherscan.io"
is_active = true
type = "standard"
source = "chainlist"

[networks.custom_mynetwork_12345]
name = "My Custom Network"
rpc_endpoint = "https://my-rpc.example.com"
chain_id = 12345
symbol = "MCN"
explorer = "https://my-explorer.example.com"
is_active = true
type = "custom"
source = "manual"
```