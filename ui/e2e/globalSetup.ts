import { execSync, spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const PORT = process.env.E2E_PORT ?? '17350';
const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..', '..');

async function waitForReady(port: string, token: string, deadlineMs: number): Promise<void> {
  const deadline = Date.now() + deadlineMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/api/agentruntime.v1.SystemService/GetVersion`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: '{}'
      });
      if (res.ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error(`daemon did not become ready on port ${port} within ${deadlineMs}ms`);
}

export default async function globalSetup(): Promise<() => Promise<void>> {
  const tmpDir = mkdtempSync(path.join(tmpdir(), 'ar-e2e-'));
  for (const d of ['runtime', 'cfg', 'data', 'state', 'ws']) mkdirSync(path.join(tmpDir, d), { recursive: true });

  execSync(`go build -o ${tmpDir}/agent-runtime-shim ./cmd/agent-runtime-shim`, { cwd: repoRoot, stdio: 'inherit' });
  execSync('npm run build:web', { cwd: path.join(repoRoot, 'ui'), stdio: 'inherit' });
  execSync('make ui-web-embed', { cwd: repoRoot, stdio: 'inherit' });
  execSync(`go build -o ${tmpDir}/agentd ./cmd/agentd`, { cwd: repoRoot, stdio: 'inherit' });

  const tokenPath = path.join(tmpDir, 'token');
  const daemon = spawn(
    path.join(tmpDir, 'agentd'),
    [
      '-foreground',
      '-data',
      path.join(tmpDir, 'd'),
      '-socket',
      path.join(tmpDir, 'a.sock'),
      '-tcp',
      `127.0.0.1:${PORT}`,
      '-token-file',
      tokenPath
    ],
    {
      env: {
        ...process.env,
        PATH: `${tmpDir}:${process.env.PATH}`,
        XDG_RUNTIME_DIR: path.join(tmpDir, 'runtime'),
        XDG_CONFIG_HOME: path.join(tmpDir, 'cfg'),
        XDG_DATA_HOME: path.join(tmpDir, 'data'),
        XDG_STATE_HOME: path.join(tmpDir, 'state')
      },
      stdio: 'inherit'
    }
  );

  await new Promise((r) => setTimeout(r, 500));
  const token = readFileSync(tokenPath, 'utf-8').trim();
  await waitForReady(PORT, token, 15000);

  writeFileSync(path.join(tmpDir, 'e2e-state.json'), JSON.stringify({ token, port: PORT, workspace: path.join(tmpDir, 'ws') }));
  process.env.E2E_STATE_FILE = path.join(tmpDir, 'e2e-state.json');

  return async () => {
    daemon.kill();
    rmSync(tmpDir, { recursive: true, force: true });
    rmSync(path.join(repoRoot, 'internal/webui/dist/assets'), { recursive: true, force: true });
    writeFileSync(
      path.join(repoRoot, 'internal/webui/dist/index.html'),
      '<!doctype html>\n<html lang="en">\n  <head>\n    <meta charset="utf-8" />\n    <title>agent-runtime</title>\n  </head>\n  <body></body>\n</html>\n'
    );
    rmSync(path.join(repoRoot, 'ui/dist-web'), { recursive: true, force: true });
  };
}
