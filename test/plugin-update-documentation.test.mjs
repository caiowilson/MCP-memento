import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

test("native plugin documentation explains staged Claude marketplace updates", async () => {
  const pluginReadme = await readFile(join(root, "plugins/memento/README.md"), "utf8");
  const englishReadme = await readFile(join(root, "README.md"), "utf8");
  const portugueseReadme = await readFile(join(root, "README.pt-BR.md"), "utf8");
  const clients = await readFile(join(root, "docs/clients.md"), "utf8");

  assert.match(pluginReadme, /plugin marketplace update memento-mcp/);
  assert.match(pluginReadme, /once per 24 hours/i);
  assert.match(pluginReadme, /new task|reload/i);
  assert.match(pluginReadme, /marketplace-update\.json/);
  assert.match(englishReadme, /staged plugin update/i);
  assert.match(portugueseReadme, /atualiza[çc][ãa]o.*agendada/i);
  assert.match(clients, /legacy marketplace.*opt/i);
  assert.doesNotMatch(pluginReadme, /0\.8\.0/);
  assert.doesNotMatch(englishReadme, /remain version-pinned and must be updated through `\/plugin` commands/i);
  assert.doesNotMatch(portugueseReadme, /continuam fixadas por versão e devem ser atualizadas pelos comandos `\/plugin`/i);
});
