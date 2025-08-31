# Requirements Document

## Introduction

Este documento define os requisitos para corrigir problemas específicos na importação e exibição de carteiras a partir de arquivos keystore V3 no BlocoWallet. Os problemas identificados incluem:

1. **Exibição incorreta do tipo de importação**: Carteiras importadas via keystore V3 mostram informações de "frase mnemônica" quando deveriam mostrar "chave privada"
2. **Barra de progresso não atualiza**: A barra de progresso da TUI para de ser atualizada durante o processo de importação
3. **Conceito incorreto sobre geração de mnemônica**: O sistema não deve tentar gerar mnemônicas a partir de keystores ou chaves privadas, pois isso é tecnicamente impossível

## Requirements

### Requirement 1

**User Story:** Como usuário, eu quero ver informações corretas sobre o tipo de importação da minha carteira, para que eu entenda claramente se minha carteira foi importada via mnemônica ou chave privada.

#### Acceptance Criteria

1. WHEN uma carteira é importada via arquivo keystore V3 THEN o sistema SHALL exibir "Tipo: Chave Privada" nos detalhes da carteira
2. WHEN uma carteira é importada via mnemônica THEN o sistema SHALL exibir "Tipo: Frase Mnemônica" nos detalhes da carteira
3. WHEN uma carteira é importada via chave privada direta THEN o sistema SHALL exibir "Tipo: Chave Privada" nos detalhes da carteira
4. WHEN o usuário visualiza a lista de carteiras THEN o sistema SHALL mostrar o tipo de importação correto para cada carteira
5. WHEN o usuário exporta uma carteira importada via keystore THEN o sistema SHALL permitir apenas exportação da chave privada, não da mnemônica

### Requirement 2

**User Story:** Como usuário, eu quero ver o progresso da importação de carteiras em tempo real, para que eu saiba que o sistema está funcionando e quanto tempo ainda resta.

#### Acceptance Criteria

1. WHEN o usuário inicia a importação de uma carteira THEN o sistema SHALL exibir uma barra de progresso que atualiza em tempo real
2. WHEN o sistema está processando o arquivo keystore THEN a barra de progresso SHALL mostrar o status atual da operação
3. WHEN o sistema está descriptografando a chave privada THEN a barra de progresso SHALL indicar esta etapa específica
4. WHEN o sistema está salvando a carteira no banco de dados THEN a barra de progresso SHALL mostrar esta etapa final
5. WHEN a importação é concluída THEN a barra de progresso SHALL mostrar 100% e uma mensagem de sucesso

### Requirement 3

**User Story:** Como usuário, eu quero informações precisas sobre minha carteira importada, para que eu não seja enganado sobre as capacidades e limitações da minha carteira.

#### Acceptance Criteria

1. WHEN uma carteira é importada via keystore V3 THEN o sistema SHALL NOT gerar ou exibir qualquer mnemônica
2. WHEN uma carteira é importada via keystore V3 THEN o sistema SHALL armazenar NULL no campo mnemônica do banco de dados
3. WHEN o usuário tenta visualizar a mnemônica de uma carteira importada via keystore THEN o sistema SHALL exibir "Mnemônica não disponível - importada via arquivo keystore"
4. WHEN o usuário tenta exportar mnemônica de carteira importada via keystore THEN o sistema SHALL exibir mensagem explicativa e oferecer exportação da chave privada
5. WHEN o sistema exibe detalhes da carteira THEN o sistema SHALL mostrar claramente que a carteira foi "Importada via Keystore V3"

### Requirement 4

**User Story:** Como usuário, eu quero feedback visual claro durante operações de importação, para que eu entenda o que está acontecendo e possa identificar problemas rapidamente.

#### Acceptance Criteria

1. WHEN a importação inicia THEN o sistema SHALL exibir "Iniciando importação..." com spinner ou barra de progresso
2. WHEN o sistema está validando o arquivo THEN o sistema SHALL exibir "Validando arquivo keystore..." 
3. WHEN o sistema está descriptografando THEN o sistema SHALL exibir "Descriptografando chave privada..."
4. WHEN o sistema está salvando THEN o sistema SHALL exibir "Salvando carteira..."
5. WHEN ocorre um erro THEN o sistema SHALL parar a barra de progresso e exibir mensagem de erro específica
6. WHEN a importação é bem-sucedida THEN o sistema SHALL exibir "Carteira importada com sucesso!" por alguns segundos

### Requirement 5

**User Story:** Como desenvolvedor, eu quero que o código seja claro sobre os tipos de importação suportados, para que não haja confusão sobre funcionalidades impossíveis como geração de mnemônica a partir de keystore.

#### Acceptance Criteria

1. WHEN o código processa importação via keystore THEN o sistema SHALL documentar claramente que mnemônicas não podem ser recuperadas
2. WHEN o código define tipos de carteira THEN o sistema SHALL distinguir claramente entre carteiras com e sem mnemônica
3. WHEN o código valida importações THEN o sistema SHALL rejeitar tentativas de gerar mnemônicas a partir de keystores
4. WHEN o código exibe informações da carteira THEN o sistema SHALL usar terminologia precisa (chave privada vs mnemônica)
5. WHEN o código registra logs THEN o sistema SHALL registrar o método de importação correto para debugging

### Requirement 6

**User Story:** Como usuário, eu quero que a interface seja consistente na terminologia usada, para que eu não fique confuso sobre os diferentes tipos de carteira.

#### Acceptance Criteria

1. WHEN a interface exibe carteiras importadas via keystore THEN o sistema SHALL usar consistentemente "Chave Privada" como tipo
2. WHEN a interface exibe carteiras criadas com mnemônica THEN o sistema SHALL usar consistentemente "Frase Mnemônica" como tipo
3. WHEN a interface oferece opções de exportação THEN o sistema SHALL mostrar apenas opções válidas para cada tipo de carteira
4. WHEN a interface exibe ajuda ou tooltips THEN o sistema SHALL explicar claramente as diferenças entre tipos de importação
5. WHEN a interface exibe erros THEN o sistema SHALL usar terminologia consistente com o tipo de importação sendo usado