# Requirements Document

## Introduction

Este documento define os requisitos para corrigir os problemas de persistência e classificação de redes no BlocoWallet. O objetivo é garantir que as configurações de rede sejam carregadas corretamente após a compilação do binário e que a classificação de redes como "custom" seja feita adequadamente baseada na existência da rede na chainlist.

## Requirements

### Requirement 1

**User Story:** Como usuário, eu quero que as configurações de rede sejam carregadas corretamente após compilar um novo binário, para que eu não perca minhas redes configuradas.

#### Acceptance Criteria

1. WHEN o usuário compila um novo binário THEN o sistema SHALL carregar as configurações de rede do arquivo config.toml usando Viper
2. WHEN o sistema inicializa THEN o sistema SHALL usar o diretório de configuração correto definido no config.toml
3. WHEN o usuário tem redes salvas no config.toml THEN o sistema SHALL exibir todas as redes configuradas na interface
4. WHEN o sistema carrega a configuração THEN o sistema SHALL usar os recursos do Viper corretamente para localizar o arquivo de configuração

### Requirement 2

**User Story:** Como usuário, eu quero que apenas redes que não existem na chainlist sejam classificadas como "custom", para que eu possa distinguir entre redes conhecidas e redes personalizadas.

#### Acceptance Criteria

1. WHEN o usuário adiciona uma rede que existe na chainlist THEN o sistema SHALL classificar a rede como rede padrão (não custom)
2. WHEN o usuário adiciona uma rede que não existe na chainlist THEN o sistema SHALL classificar a rede como "custom"
3. WHEN o sistema gera chaves para redes THEN o sistema SHALL usar prefixos apropriados baseados na classificação da rede
4. WHEN o usuário visualiza a lista de redes THEN o sistema SHALL indicar claramente quais redes são custom e quais são padrão

### Requirement 3

**User Story:** Como desenvolvedor, eu quero que o sistema use corretamente os recursos do Viper para gerenciamento de configuração, para garantir consistência e confiabilidade no carregamento de configurações.

#### Acceptance Criteria

1. WHEN o sistema carrega configurações THEN o sistema SHALL usar Viper para localizar e carregar o arquivo config.toml
2. WHEN o sistema salva configurações THEN o sistema SHALL manter a compatibilidade com o formato de configuração do Viper
3. WHEN o sistema inicializa THEN o sistema SHALL respeitar as configurações de diretório definidas no config.toml
4. WHEN ocorre erro no carregamento THEN o sistema SHALL fornecer mensagens de erro claras sobre problemas de configuração

### Requirement 4

**User Story:** Como usuário, eu quero que o sistema valide redes contra a chainlist antes de classificá-las, para garantir precisão na categorização.

#### Acceptance Criteria

1. WHEN o usuário adiciona uma nova rede THEN o sistema SHALL consultar a chainlist para verificar se a rede existe
2. WHEN a rede existe na chainlist THEN o sistema SHALL usar informações da chainlist para preencher dados faltantes
3. WHEN a rede não existe na chainlist THEN o sistema SHALL permitir que o usuário forneça todos os detalhes manualmente
4. WHEN o sistema não consegue acessar a chainlist THEN o sistema SHALL permitir adicionar a rede como custom com aviso apropriado