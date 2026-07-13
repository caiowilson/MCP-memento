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
- Claude Code, Claude Desktop e outros clientes MCP: [`docs/clients.md`](./docs/clients.md)
- Uso com VS Code: [`docs/vscode.md`](./docs/vscode.md)
- Extensão VS Code: [`vscode-extension/README.md`](./vscode-extension/README.md)
- Guia de ADR: [`docs/adr/README.md`](./docs/adr/README.md)
- Índice e decisões de ADR: [`docs/adr/ADRs.md`](./docs/adr/ADRs.md)

## Instalação

### Plugin do Claude Code (recomendado)

Adicione este repositório como marketplace do Claude Code, instale o Memento e recarregue os plugins ativos:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

O plugin habilitado inicia o Memento automaticamente em cada projeto. Na primeira inicialização, ele baixa o binário pré-compilado e versionado para macOS, Linux ou Windows em x64 ou arm64, verifica o checksum SHA-256 da release e o armazena no diretório persistente de dados do plugin. O primeiro uso requer acesso ao GitHub; os próximos verificam o cache e funcionam offline.

### Binário independente

Instale a versão mais recente do servidor pré-compilado em `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/caiowilson/MCP-memento/main/install.sh | sh
```

Defina `MEMENTO_INSTALL_DIR` para escolher outro diretório. O instalador suporta macOS, Linux e ambientes Windows com shell POSIX em x64 e arm64. Garanta que o diretório escolhido esteja no `PATH` e verifique o binário:

```bash
memento-mcp help
```

Atualize uma instalação independente no próprio local:

```bash
memento-mcp update
```

Use `memento-mcp update --check` para apenas verificar, sem instalar. Builds de release também fazem uma verificação silenciosa e limitada a uma vez por dia ao iniciar o servidor, escrevendo um aviso somente no stderr quando houver atualização; mensagens de atualização nunca são escritas no stdout do MCP. Defina `MEMENTO_UPDATE_CHECK=false` para desativar essa verificação. Instalações pelo plugin do Claude Code continuam fixadas por versão e devem ser atualizadas pelos comandos `/plugin`.

### Compilação a partir do código-fonte

A compilação requer Go 1.25.5 ou mais recente:

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
make build
./bin/memento-mcp help
```

## Uso com Claude Code

Usuários do plugin não precisam de configuração MCP adicional. Verifique o servidor iniciado automaticamente com `/mcp`. Atualize com `/plugin marketplace update memento-mcp`, depois `/plugin update memento@memento-mcp` e `/reload-plugins`.

Os nomes MCP do plugin têm namespace; por exemplo, o prompt prime é `/mcp__plugin_memento_memento__prime`.

### Configuração MCP manual

Se você instalou o binário independente ou compilou a partir do código-fonte, execute este comando no projeto que o Claude Code deve indexar. Substitua o caminho do executável caso ele não esteja no `PATH`:

```bash
claude mcp add memento -- memento-mcp
```

O Claude Code informa o projeto ativo por `CLAUDE_PROJECT_DIR`, então o memento o indexa sem uma chamada manual a `repo_switch_workspace`. Verifique a conexão com `claude mcp list` ou `/mcp` dentro do Claude Code.

Para uma configuração manual compartilhada e versionável, adicione este `.mcp.json` à raiz do projeto:

```json
{
  "mcpServers": {
    "memento": {
      "type": "stdio",
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

Essa forma exige um executável em `bin/memento-mcp` em cada checkout. O Claude Code pede que cada usuário aprove servidores com escopo de projeto antes do primeiro uso.

### Claude Desktop

Adicione a entrada stdio equivalente ao `claude_desktop_config.json` e reinicie o Claude Desktop:

```json
{
  "mcpServers": {
    "memento": {
      "command": "/caminho/absoluto/para/MCP-memento/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

No macOS ou WSL, `claude mcp add-from-claude-desktop` pode importar esse servidor para o Claude Code. Consulte o [guia de configuração de clientes](./docs/clients.md) para mais detalhes.

## O Que Faz

- Expõe ferramentas MCP para operações no repositório: `repo_list_files`, `repo_read_file`, `repo_search`, `repo_related_files`, `repo_context`, `repo_switch_workspace`
- Mantém um índice de código em disco por repositório para recuperação de contexto rápida e com limites definidos
- Armazena notas explícitas com escopo do repositório: `memory_upsert`, `memory_search`, `memory_clear`
- Suporta uma extensão complementar para VS Code que instala e configura o servidor

## Como Funciona

1. O servidor inicia via stdio JSON-RPC e registra as ferramentas MCP.
2. Ele detecta automaticamente a raiz do workspace (`--root`, `CLAUDE_PROJECT_DIR`, MCP `roots/list` e depois cwd) e cria um índice local de chunks em `~/.memento-mcp/`.
3. A detecção de mudanças é incremental:
   - Repositórios Git: polling de `git status` (caminho rápido)
   - Repositórios sem Git: fallback com watcher do sistema de arquivos
4. As ferramentas de contexto combinam:
   - Chunks indexados e pontuação
   - Limites de chunk alinhados a declarações Go e JavaScript/TypeScript, com fallback limitado por linhas
   - Relacionamentos com conhecimento de linguagem (análise de tipos Go, imports TS/JS e referências Composer/símbolos/Laravel em PHP)
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
- Node.js (se for trabalhar em `vscode-extension/` ou no plugin do Claude Code)

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

## Fluxo de Trabalho Recomendado (memória + contexto enxuto)

Trate o Memento como o padrão tanto para memória quanto para contexto em um repositório:

- **Prefira a memória do Memento a qualquer outro armazenamento de memória.** Persista decisões duráveis e handoffs com `memory_upsert` (ancorado ao código); recupere com `memory_search` / `memory_list` antes de re-derivar. `memory_gc` / `memory_delete` / `memory_clear` são destrutivos — use apenas com instrução explícita.
- **Prepare o índice do repositório para um contexto mais enxuto e menos tokens.** Comece com `repo_context` no arquivo ativo, `repo_outline` para assinaturas, `repo_search` para símbolos e `repo_related_files` para imports — recorra a `repo_read_file` apenas para o caminho exato de que você precisa. Consultar o índice primeiro (e ler arquivos completos por último) é a principal forma de reduzir o uso de tokens.

Para tornar isso automático em um projeto, execute `memento-mcp claude-md` na raiz dele: o comando grava esta seção em `./CLAUDE.local.md` para que a orientação seja carregada em toda sessão. Rode novamente para atualizar o bloco no lugar; use `--print-only` para pré-visualizar.
