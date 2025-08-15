# Requirements Document

## Introduction

Este documento define os requisitos para corrigir dois bugs críticos na funcionalidade de importação de carteiras do BlocoWallet:

1. **Bug de Duplicata em Importação via Mnemônica**: Quando o usuário tenta importar múltiplas carteiras usando frases mnemônicas distintas, o sistema incorretamente reporta que a carteira já existe, mesmo quando as mnemônicas são diferentes e representam carteiras distintas.

2. **Bug de Geração Incorreta de Mnemônica a partir de Chave Privada**: Quando o usuário importa uma carteira através de chave privada, o sistema está gerando uma mnemônica "determinística" e retornando-a como se fosse a mnemônica original da carteira, o que é tecnicamente impossível e conceitualmente incorreto.

## Requirements

### Requirement 1: Correção da Verificação de Duplicatas para Importação via Mnemônica

**User Story:** Como usuário, eu quero importar múltiplas carteiras usando diferentes frases mnemônicas, para que eu possa gerenciar todas as minhas carteiras distintas no BlocoWallet, mesmo que algumas derivem para o mesmo endereço Ethereum.

#### Acceptance Criteria

1. WHEN o usuário importa uma carteira via mnemônica THEN o sistema SHALL verificar duplicatas baseado na combinação de endereço E mnemônica criptografada
2. WHEN duas mnemônicas diferentes derivam para o mesmo endereço Ethereum THEN o sistema SHALL permitir a importação de ambas as carteiras como entradas separadas
3. WHEN o usuário tenta importar a mesma mnemônica novamente THEN o sistema SHALL rejeitar a importação com mensagem "Carteira com esta mnemônica já existe"
4. WHEN o usuário importa mnemônicas diferentes que derivam endereços diferentes THEN o sistema SHALL permitir a importação normalmente
5. WHEN o sistema verifica duplicatas THEN o sistema SHALL comparar as mnemônicas descriptografadas para determinar se são idênticas

### Requirement 2: Remoção da Geração Incorreta de Mnemônica para Importação via Chave Privada

**User Story:** Como usuário, eu quero importar uma carteira através de chave privada sem receber uma mnemônica falsa, para que eu tenha informações precisas sobre minha carteira e não seja enganado sobre suas propriedades.

#### Acceptance Criteria

1. WHEN o usuário importa uma carteira via chave privada THEN o sistema SHALL NOT gerar ou retornar qualquer mnemônica
2. WHEN o usuário importa uma carteira via chave privada THEN o sistema SHALL armazenar NULL ou string vazia no campo mnemônica do banco de dados
3. WHEN o usuário importa uma carteira via chave privada THEN o sistema SHALL retornar apenas endereço, chave pública e chave privada nos detalhes da carteira
4. WHEN o usuário visualiza uma carteira importada via chave privada THEN o sistema SHALL indicar claramente que não há mnemônica disponível
5. WHEN o usuário tenta exportar uma carteira importada via chave privada THEN o sistema SHALL permitir exportação apenas da chave privada, não da mnemônica

### Requirement 3: Atualização da Estrutura de Dados para Suportar Carteiras sem Mnemônica

**User Story:** Como desenvolvedor, eu quero que o sistema suporte carteiras que não possuem mnemônica, para que possamos armazenar corretamente carteiras importadas via chave privada.

#### Acceptance Criteria

1. WHEN uma carteira é criada via chave privada THEN o campo mnemônica SHALL ser opcional (nullable)
2. WHEN uma carteira é criada via mnemônica THEN o campo mnemônica SHALL ser obrigatório e criptografado
3. WHEN o sistema consulta carteiras THEN o sistema SHALL distinguir entre carteiras com e sem mnemônica
4. WHEN o sistema valida carteiras THEN o sistema SHALL aceitar carteiras com mnemônica NULL para importações via chave privada
5. WHEN o sistema migra dados existentes THEN o sistema SHALL preservar todas as mnemônicas existentes

### Requirement 4: Melhoria da Verificação de Duplicatas Baseada no Tipo de Importação

**User Story:** Como usuário, eu quero que o sistema verifique duplicatas de forma inteligente baseada no método de importação, para que eu não tenha problemas desnecessários ao gerenciar minhas carteiras.

#### Acceptance Criteria

1. WHEN o usuário importa via mnemônica THEN o sistema SHALL verificar duplicatas comparando mnemônicas descriptografadas
2. WHEN o usuário importa via chave privada THEN o sistema SHALL verificar duplicatas comparando apenas endereços Ethereum
3. WHEN existe uma carteira com o mesmo endereço mas importada via método diferente THEN o sistema SHALL permitir a coexistência se os dados de origem forem diferentes
4. WHEN o usuário tenta importar a mesma chave privada novamente THEN o sistema SHALL rejeitar com mensagem "Carteira com esta chave privada já existe"
5. WHEN o sistema detecta conflito de duplicata THEN o sistema SHALL fornecer mensagem específica indicando o tipo de conflito encontrado

### Requirement 5: Atualização das Mensagens de Erro e Interface do Usuário

**User Story:** Como usuário, eu quero receber mensagens claras sobre o status das minhas carteiras importadas, para que eu entenda exatamente que tipo de informação está disponível para cada carteira.

#### Acceptance Criteria

1. WHEN uma carteira importada via chave privada é exibida THEN o sistema SHALL mostrar "Mnemônica: Não disponível (importada via chave privada)"
2. WHEN uma carteira importada via mnemônica é exibida THEN o sistema SHALL mostrar a opção de visualizar/exportar mnemônica
3. WHEN o usuário tenta uma operação que requer mnemônica em carteira importada via chave privada THEN o sistema SHALL exibir mensagem explicativa apropriada
4. WHEN ocorre erro de duplicata THEN o sistema SHALL especificar se o conflito é por mnemônica ou chave privada
5. WHEN o usuário visualiza detalhes da carteira THEN o sistema SHALL indicar claramente o método de importação usado

### Requirement 6: Suporte Universal a KDF para Importação de KeyStore V3

**User Story:** Como usuário, eu quero importar qualquer arquivo KeyStore V3 válido independentemente dos parâmetros KDF, tipos de dados JSON ou variações de nomenclatura, para que eu possa usar carteiras criadas por diferentes aplicações sem problemas de compatibilidade.

#### Acceptance Criteria

1. WHEN o usuário importa um KeyStore com parâmetros KDF em diferentes tipos JSON (int, float64, string) THEN o sistema SHALL converter automaticamente para o tipo correto
2. WHEN o usuário importa um KeyStore com variações de nomenclatura KDF (scrypt, Scrypt, SCRYPT, pbkdf2, PBKDF2) THEN o sistema SHALL normalizar e processar corretamente
3. WHEN o usuário importa um KeyStore com parâmetros KDF em diferentes nomes (n/N/cost, r/R/blocksize, p/P/parallel) THEN o sistema SHALL reconhecer todas as variações
4. WHEN o usuário importa um KeyStore com salt em diferentes formatos (hex string, array de bytes, string direta) THEN o sistema SHALL processar corretamente
5. WHEN o sistema detecta parâmetros KDF inseguros THEN o sistema SHALL validar e alertar sobre o nível de segurança
6. WHEN o usuário importa um KeyStore com KDF não suportado THEN o sistema SHALL fornecer mensagem de erro específica e sugestões
7. WHEN o sistema processa um KeyStore THEN o sistema SHALL registrar informações detalhadas sobre o KDF usado para debugging

### Requirement 7: Análise de Compatibilidade e Segurança de KeyStore

**User Story:** Como usuário, eu quero receber informações sobre a compatibilidade e segurança do meu KeyStore durante a importação, para que eu entenda os riscos e características da minha carteira.

#### Acceptance Criteria

1. WHEN o usuário importa um KeyStore THEN o sistema SHALL analisar a compatibilidade e gerar um relatório detalhado
2. WHEN o sistema detecta parâmetros de segurança baixa THEN o sistema SHALL alertar o usuário com recomendações específicas
3. WHEN o sistema processa um KeyStore THEN o sistema SHALL classificar o nível de segurança (Low, Medium, High, Very High)
4. WHEN o usuário visualiza detalhes da carteira importada THEN o sistema SHALL mostrar informações sobre o KDF usado e nível de segurança
5. WHEN o sistema encontra configurações não padrão THEN o sistema SHALL documentar as variações encontradas

### Requirement 8: Testes Automatizados para os Cenários Corrigidos e Universal KDF

**User Story:** Como desenvolvedor, eu quero testes automatizados abrangentes para os cenários de importação corrigidos e suporte Universal KDF, para garantir que os bugs não reapareçam e que qualquer KeyStore válido possa ser importado.

#### Acceptance Criteria

1. WHEN os testes são executados THEN o sistema SHALL testar importação de múltiplas mnemônicas diferentes que derivam o mesmo endereço
2. WHEN os testes são executados THEN o sistema SHALL testar importação de carteira via chave privada sem geração de mnemônica
3. WHEN os testes são executados THEN o sistema SHALL testar verificação de duplicatas para ambos os métodos de importação
4. WHEN os testes são executados THEN o sistema SHALL testar coexistência de carteiras com mesmo endereço mas métodos de importação diferentes
5. WHEN os testes são executados THEN o sistema SHALL testar mensagens de erro específicas para cada tipo de conflito de duplicata
6. WHEN os testes são executados THEN o sistema SHALL testar importação de KeyStores com diferentes tipos de dados JSON (int, float64, string)
7. WHEN os testes são executados THEN o sistema SHALL testar importação de KeyStores com variações de nomenclatura KDF
8. WHEN os testes são executados THEN o sistema SHALL testar importação de KeyStores com diferentes formatos de salt
9. WHEN os testes são executados THEN o sistema SHALL testar validação de segurança para diferentes configurações KDF
10. WHEN os testes são executados THEN o sistema SHALL testar análise de compatibilidade para KeyStores de diferentes origens (Geth, MetaMask, Trust Wallet, etc.)