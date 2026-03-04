# memento-mcp

[![Release do Servidor](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=server%2Fv*&label=server)](https://github.com/caiowilson/MCP-memento/releases)
[![Tag Binária Mais Recente](https://img.shields.io/badge/tag-server%2Flatest-blue)](https://github.com/caiowilson/MCP-memento/releases/tag/server%2Flatest)
[![Release da Extensão VS Code](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=extension%2Fv*&label=extension)](https://github.com/caiowilson/MCP-memento/releases)
[![Versão do Go](https://img.shields.io/badge/Go-1.25.5-00ADD8?logo=go)](https://go.dev/)
[![Licença: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

Um servidor MCP local-first que oferece aos agentes de IA uma memória durável e de alto sinal para o seu repositório: contexto de código indexado, relacionamentos semânticos, busca rápida e notas explícitas que persistem entre sessões.

## Idiomas

- Inglês: `README.md`
- Português do Brasil: [`README.pt-BR.md`](./README.pt-BR.md)

## Documentação

- Docs do projeto: [`docs/README.md`](./docs/README.md)
- Uso com VS Code: [`docs/vscode.md`](./docs/vscode.md)
- Extensão VS Code: [`vscode-extension/README.md`](./vscode-extension/README.md)
- Guia de ADR: [`docs/adr/README.md`](./docs/adr/README.md)
- Índice e decisões de ADR: [`docs/adr/ADRs.md`](./docs/adr/ADRs.md)

## O Que Faz

- Expõe ferramentas MCP para operações no repositório: `repo_list_files`, `repo_read_file`, `repo_search`, `repo_related_files`, `repo_context`
- Mantém um índice de código em disco por repositório para recuperação de contexto rápida e com limites definidos
- Armazena notas explícitas com escopo do repositório: `memory_upsert`, `memory_search`, `memory_clear`
- Suporta uma extensão complementar para VS Code que instala e configura o servidor

## Como Funciona

1. O servidor inicia via stdio JSON-RPC e registra as ferramentas MCP.
2. Ele cria e atualiza um índice local de chunks em `~/.memento-mcp/`.
3. A detecção de mudanças é incremental:
   - Repositórios Git: polling de `git status` (caminho rápido)
   - Repositórios sem Git: fallback com watcher do sistema de arquivos
4. As ferramentas de contexto combinam:
   - Chunks indexados e pontuação
   - Relacionamentos com conhecimento de linguagem (Go, TS/JS, PHP)
   - Limites rígidos de bytes e linhas para segurança do contexto de LLM
5. Notas explícitas são armazenadas separadamente como memória durável com escopo do repositório.

## Estrutura do Projeto

- `cmd/server/` - ponto de entrada
- `internal/mcp/` - servidor MCP e handlers das ferramentas
- `internal/indexing/` - chunking, manifesto, busca, indexação incremental
- `internal/app/` - wiring do ciclo de vida da aplicação
- `vscode-extension/` - extensão complementar (instalador e UX de configuração MCP)
- `docs/` - documentação de uso e ADRs

## Contribuição

### Pré-requisitos

- Go `1.25.5`
- Node.js (somente se for trabalhar em `vscode-extension/`)

### Desenvolvimento Local

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
make build
./bin/memento-mcp
```

### Rodar Testes

```bash
go test ./...
```

### Desenvolvimento da Extensão VS Code

```bash
cd vscode-extension
npm install
npm run build
```

### Fluxo de Contribuição

1. Crie uma branch a partir da `main`.
2. Faça mudanças focadas com atualização de testes e docs.
3. Rode `go test ./...` (e build/testes da extensão quando aplicável).
4. Abra um PR com:
   - Descrição do problema
   - Abordagem
   - Passos de validação
   - Qualquer mudança de ferramenta ou comportamento

## Temas de Roadmap

- Melhor qualidade e ranqueamento de contexto
- Suporte semântico mais amplo para linguagens
- Melhorias de UX da extensão e confiabilidade de instalação
- Automação de release e ferramentas operacionais
