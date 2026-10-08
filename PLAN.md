# PLAN — Restauração do componente UI do bloco-wallet

> Arquivo de plano de trabalho. Define agenda, riscos, critérios de aceitação e timeline para que o componente UI (TUI + serviço de carteiras) volte a compilar, testar e funcionar normalmente no estado atual do repositório (branch `main`, commit `b1fa358`).

> **Resultado (registrado após implementação):** a Proposta A foi avaliada e **rejeitada** — ver seção 4.1. A implementação seguiu a Alternativa B estendida: os handlers/renderers legacy órfãos foram removidos e a migração `WalletVault` foi concluída (`fix/ui-legacy-wallet-service-cleanup`, PR #58). O documento abaixo é mantido como registro do diagnóstico original; onde diverge do que foi implementado, a seção 4.1 prevalece.

---

## 1. Objetivo

Restaurar o estado onde o componente `internal/ui` (TUI) + `internal/wallet.WalletService` compile, passem em `go vet`, `go test` (rápido e produção) e o fluxo humano de carteira (criar, importar, listar, deletar, detalhes) funcione via TUI, como era o esperado no branch atual (`main`, commit `b1fa358`).

---

## 2. Estado atual (diagnóstico)

### 2.1 Erro de compilação / vet no `internal/ui`

`go build ./internal/ui/...` e `go vet ./internal/ui/...` falham com erros em `tui.go` (linhas ~1154–1206):

```
internal/ui/tui.go:1154:36: m.Service.CreateWalletFromMnemonic undefined
                          (type *wallet.WalletService has no field or method CreateWalletFromMnemonic)
internal/ui/tui.go:1197:32: m.textInputs undefined
internal/ui/tui.go:1197:45: m.importStage undefined
internal/ui/tui.go:1203:6:  m.importWords undefined
internal/ui/tui.go:1203:20: m.importStage undefined
internal/ui/tui.go:1204:6:  m.textInputs undefined
internal/ui/tui.go:1205:6:  m.importStage undefined
internal/ui/tui.go:1206:9:  m.importStage undefined
internal/ui/tui.go:1206:29: m.textInputs undefined
```

### 2.2 Causa raiz identificada

A sessão anterior (antes da correção de higiene de UI) reescreveu `internal/wallet/wallet_service.go`, removendo **5 funções** da API de serviço (280 linhas removidas, 5 funcs):

| Função removida | Usada por `tui.go` | Ref. no HEAD |
|---|---|---|
| `CreateWallet(name, password)` | indireta | 59–66 |
| `CreateWalletFromMnemonic(name, mnemonic, password)` | `tui.go:1154` | 67–128 |
| `ImportWallet(name, mnemonic, password)` | `tui.go:1259` | 129–194 |
| `ImportWalletFromPrivateKey(name, privateKeyHex, password)` | `tui.go:1254` | 195–302 |
| `LoadWallet(wallet *Wallet, password string)` | `tui.go:1856` | 777–809 |

E também **3 campos do `CLIModel`** usados em `tui.go` mas inexistentes:

| Campo | Uso em `tui.go` |
|---|---|
| `textInputs []textinput` | 1197–1207, 1224, 1710–1711 |
| `importStage int` | 1197–1207, 1224 |
| `importWords []string` *(o plano original dizia `map[int]string` — incorreto; `strings.Join(m.importWords, " ")` no HEAD prova o slice)* | 1203, 1258, 1713–1714 |

**Confirmação de pré-existente:** `git stash` dos três arquivos + `go vet ./internal/ui/` reproduz o mesmo erro (`tui.go:1176`/`1154`), ou seja, o bloqueio de build **não é causado por minhas correções de higiene** (que removeram código morto: constantes, maps e dispatch — essas restaram compile-clean, `gofmt` limpo, 0 referências mortas).

### 2.3 O que já foi feito anteriormente (contexto)

- Correção de higiene de UI concluída e compile-clean: constantes/métodos/mapas/renderizadores de importação legacy removidos (`ImportWalletView`, `ImportPrivateKeyView`, `ImportKeystoreView`, `WalletPasswordView`, `viewWalletPassword`, dispatch, mapas de views).
- `internal/ui` não compila mais por causa dos métodos removidos do `WalletService` e dos campos de `CLIModel` (acima).
## 3. Escopo da correção

**Arquivos envolvidos:**
- `internal/wallet/wallet_service.go` — restaurar a surface API que `tui.go` usa.
- `internal/ui/tui.go` — restaurar os campos `CLIModel` (`textInputs`, `importStage`, `importWords`) e (opcional) alinhar handlers legacy.
- `internal/wallet/wallet_service_test.go` — cobertura dos métodos restaurados.
- `go.mod` / `go.sum` — **não alterar** (sigma/verificação futura).
- `Makefile`, `docs/`, `.gitignore` — apenas se necessário nos testes finais.

**Não é prioridade / fora do escopo:**
- Multisig `safe`, keystore novo, base de dados, Ledger/Trezor, build cruzado, docs extensos, frontend web.

---

## 4. Proposta de correção (recomendada)

**Abordagem A — Restauração da surface API (menor impacto, volta o componente a funcionar)**
Recuperar os métodos removidos no `WalletService` com exatamente as mesmas assinaturas da HEAD, implementando pelo código atual (rotas V3/kdf) para evitar duplicação. O `CLIModel` recupera os 3 campos removidos. Assim `tui.go` compila e os fluxos (criar, importar, carregar, deletar) funcionam normalmente.

**Alternativa B (menor escopo, mas mais risco)**
Reescrever os handlers de importação no `tui.go` para usar apenas a nova API V3 (`ImportWalletFromKeystoreV3WithContext`), mas isso requer refatorar o fluxo de import (mnemonic, privateKey, keystore) — não recomendado para regressão imediata.

> Recomendação: Proposta A, pois resolve o build, mantém todos os fluxos humanos e não depende de decisão arquitetural nova.

### 4.1 Decisão registrada — Alternativa B executada (Proposta A inviável)

Durante a implementação, a Proposta A mostrou-se **inviável e insegura**:

1. **Infraestrutura removida**: os métodos legacy dependem de `EncryptMnemonic`, `DecryptMnemonic`, `CryptoService`, `InitCryptoService` e `defaultCryptoService`, todos removidos de `crypto.go` na migração para o envelope Argon2id do `WalletVault`. Restaurá-los exigiria reescrever o stack de criptografia anterior.
2. **Vulnerabilidade real**: `ImportWalletFromPrivateKey` usava `GetTestKeystoreParams()` — parâmetros scrypt de teste aplicados a keystores de produção.
3. **`m.Service` nunca é populado em produção**: `NewCLIModel(vault)` recebe apenas `*WalletVault`; os métodos restaurados seriam dead code e cada call site um nil-pointer panic em potencial.
4. **O fluxo vivo já usa `m.Vault`**: canonical import, create com backup challenge, list e details operam via `WalletVault`.

**Executado:** remoção dos handlers/renderers órfãos (`updateImportWallet*`, `updateImportPrivateKey`, `updateImportKeystore`, `updateWalletPassword`, `initWalletPassword`, `initEnhancedImport`, o subsistema enhanced-import completo — state, file picker, listeners e testes dedicados), do campo `CLIModel.Service` e dos campos legacy de import; `walletCountCmd`/`refreshWalletsTable` convertidos para vault-only; `EnhancedImportView` e `GetContentView` removidos. Nenhum método legacy foi restaurado.

---

## 5. Checklist de implementação

### 5.1 Preparação
- [ ] `git checkout main` (ou branch feature separada `feat/restore-wallet-service-api`); confirmar `git log -1`.
- [ ] Snapshot do estado atual: `git status --short > /tmp/pre_plan_state.txt` (para rollback).
- [ ] Go ≥ 1.26.7 disponível; `make deps` rodado.

### 5.2 Restauração na `internal/wallet/wallet_service.go`
- [ ] `NewWalletService(repo WalletRepository, ks *keystore.KeyStore, cfg *config.Config, keyStoreDir ...string)` — confirmar que a assinatura com `cfg` está correta (verificar quem chama `NewWalletService` no repo).
- [ ] Adicionar `func (ws *WalletService) CreateWallet(name, password string) (*WalletDetails, error)` — delega para `CreateWalletFromMnemonic` com mnemonic gerada.
- [ ] Adicionar `func (ws *WalletService) CreateWalletFromMnemonic(name, mnemonic, password string) (*WalletDetails, error)` — validar mnemonic (DetectBIP39Language), prosseguir para criação.
- [ ] Adicionar `func (ws *WalletService) ImportWallet(name, mnemonic, password string)` — validar, checar duplicidade, importar.
- [ ] Adicionar `func (ws *WalletService) ImportWalletFromPrivateKey(name, privateKeyHex, password string)` — normalize `0x`, importar.
- [ ] Adicionar `func (ws *WalletService) LoadWallet(wallet *Wallet, password string) (*WalletDetails, error)` — ler keyJSON, descriptografar, montar `WalletDetails`.
- [ ] Todos os métodos devem usar o `cfg` do recebedor (`ws.cfg`) se necessário (KDF, parâmetros de criptografia) — manter consistência com V3.
- [ ] Garantir imports de pacotes internos não presentes (ex.: `blocowallet/pkg/config`, storage, keystore, localization) sem gerar ciclos de dependência.

### 5.3 Restauração no `internal/ui/tui.go`
- [ ] `textInputs []textinput` no `CLIModel` (zeropara uso seguro via `make([]textinput, 0, n)`).
- [ ] `importStage int` (inicializar em 0 no construtor/inicialização do model).
- [ ] `importWords []string` (inicializar como slice; o tipo `map[int]string` citado na proposta original estava incorreto).
- [ ] `clearImportSecrets()` — verificar que percorre `m.textInputs` e `m.importWords` com `range m.textInputs`, `range m.importWords` (já existe; validar).
- [ ] Handlers legacy (updateImportWallet etc.) — verificar se são chamados por algum dispatch; se não forem, pode deixar como "dead" ou remover depois (opcional, para limpeza).
- [ ] `go vet` / `gofmt` nesses arquivos.

### 5.4 Correção de dependências cruzadas (se necessário)
- [ ] Qualquer chamador de `NewWalletService` no repo que não passa `cfg` (ex.: `internal/daemon`, `cmd/blocowallet`, testes) — atualizar chamada ou adicionar valor default.
- [ ] Verificar `internal/wallet/` testes existentes (`wallet_service_test.go`, mocks) — não quebrar.

## 6. Testes

### 6.1 Unidade rápida (obrigatória)
- [ ] `go build ./...` (todo o módulo)
- [ ] `go vet ./internal/ui/ ./internal/wallet/ ./internal/constants/`
- [ ] `go test ./internal/wallet/... -count=1`
- [ ] `go test ./internal/ui/... -count=1`

### 6.2 Produção / parâmetros otimizados
- [ ] `go test -tags=production ./internal/wallet/... ./internal/ui/... -count=1`
- [ ] `CGO_ENABLED=1 go test ./internal/wallet/... -race` (SQLite CGO)

### 6.3 Cobertura de novos símbolos
- [ ] Adicionar testes para `CreateWallet`, `CreateWalletFromMnemonic`, `ImportWallet`, `ImportWalletFromPrivateKey`, `LoadWallet` no `wallet_service_test.go` (incluir: mnemonic inválida lança `NewInvalidImportDataError`, senha inválida, carregamento de wallet existente).
- [ ] Verificar cobertura de `WalletService` e de `tui.go` (novos campos).

---

## 7. Validação de fumaça do TUI

Após build OK, executar o binário gerado (`build/bloco-wallet` ou `go run ./cmd/blocowallet`) com seed de teste do `internal/wallet/testdata`:

- [ ] Menu inicial lista `ListWallets` e navegação.
- [ ] Criação de carteira (CreateWallet → mnemonic → backup) sem pânico.
- [ ] Importar: mnemonic, privateKey, keystore (fluxo legacy restaurado) e fluxo V3.
- [ ] Abrir `wallet_details`, `account_history`, deletar wallet.
- [ ] Pressões de teclado e `esc` no menu não trazem panic (verificar `m.currentView` default).
- [ ] Verificar que `debug.log` não é gerado (ou removido do `.gitignore` caso exista).

---

## 8. Segurança e compliance

- [ ] **Nunca** commitar chaves privadas, keystores reais ou mnemônicos. Usar apenas `internal/wallet/testdata` (teste) e seeds aleatórias.
- [ ] `make fmt` + `make lint` (golangci-lint) braços verdes.
- [ ] `govulncheck` (execução oficial no CI; não é local — manter o job CI independente do Trivy).
- [ ] Declaração no commit: `feat: restore WalletService API surface for UI build` (Conventional Commits).
- [ ] Arquivos de configuração de build/test (`.gitignore`) — não commitar `debug.log` se ele for gerado.

## 9. Risco e tempo estimado

- **Risco:** baixo/moderado — somente restauração de API removida; nenhuma mudança de comportamento de criptografia; risco de compile-only se a API V3 for usada nos handlers novos.
- **Tempo estimado:** 2–3 horas para implementação + ~1 hora de testes e fumaça TUI.
- **Orçamento:** 1 a 2 sessões; priorizar primeiro o build (`go build ./...`), depois os testes.

---

## 10. Critérios de aceitação (definindo "funciona normalmente")

- [ ] `go build ./...` ✅
- [ ] `go vet ./...` ✅
- [ ] `go test ./... -count=1` ✅
- [ ] `go test -tags=production ./... -count=1` ✅
- [ ] TUI de fumaça: criar/importar/listar/deletar sem panic
- [ ] Compilação da UI com os 5 métodos restaurados + 3 campos CLIModel
- [ ] `make fmt` e `make lint` sem alterações de qualidade

---

## 11. Entregáveis e entrega

- [ ] `PLAN.md` gerado nesta reunião (documento de referência).
- [ ] PR com: correção de `wallet_service.go`, correção de `tui.go`, testes, atualização da documentação se aplicável.
- [ ] Checklist no PR: build, vet, test, cobertura, segurança, docs.
- [ ] Screenshots/GIFs do TUI (se a UI mudar visualmente).

---

## 12. Anexos

### Anexo A — Evidências de diagnóstico

```
# Verificar build atual
go build ./internal/ui/...        # falha: CreateWalletFromMnemonic undefined
go vet ./internal/ui/...          # mesmo erro em tui.go:1154–1206

# Estado exato do repositório
git log --oneline -3
# b1fa358 feat: streamline keystore imports and reflect release versions in TUI
# (working tree contém o estado anterior + correções de higiene UI)

# Funções removidas (evidência do git diff)
-func (ws *WalletService) CreateWallet(name, password string)
-func (ws *WalletService) CreateWalletFromMnemonic(name, mnemonic, password string)
-func (ws *WalletService) ImportWallet(name, mnemonic, password string)
-func (ws *WalletService) ImportWalletFromPrivateKey(name, privateKeyHex, password string)
-func (ws *WalletService) LoadWallet(wallet *Wallet, password string)
```

### Anexo B — Arquitetura relevante

- `internal/ui/cli_model.go` — `CLIModel` (estado da TUI; contém `Service *WalletService`, `currentView`, `styles`, etc.).
- `internal/ui/tui.go` — `Update()` (switch de views), `getContentView()` (mapa), handlers por view (criar/importar).
- `internal/ui/views.go` — mapa `viewNames` e renderizadores.
- `internal/wallet/wallet_service.go` — `WalletService` (interface impl; métodos v2/V3).
- `internal/blockchain/rpc_gateway.go`, `internal/daemon/listen_unix.go` — não afetados por este plano.

