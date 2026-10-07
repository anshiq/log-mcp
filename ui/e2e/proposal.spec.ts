import { test, expect } from '@playwright/test';
import { e2eInfo, loginUrl } from './state';

test('proposal banner appears with approve and dismiss', async ({ page }) => {
  const info = e2eInfo();
  await page.route('**/agentruntime.v1.ConfigService/GetConfig', async (route) => {
    const res = await route.fetch();
    let body: unknown = {};
    try {
      body = await res.json();
    } catch {
      body = {};
    }
    const b = body as Record<string, unknown>;
    b['pendingProposal'] = {
      id: 'test-proposal-1',
      yaml: 'version: 3\napps:\n  web:\n    command: [npm, run, dev]\n',
      summary: [{ app: 'web', action: 'update', reason: 'detected command differs' }],
      createdAt: Date.now() / 1000
    };
    b['configSource'] = 'db';
    await route.fulfill({ response: res, json: b });
  });
  await page.route('**/agentruntime.v1.ConfigService/ResolveProposal', async (route) => {
    await route.fulfill({ json: { applied: true, revision: 99 } });
  });
  await page.goto(loginUrl(info));
  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open' }).click();
  await page.getByRole('link', { name: 'Config' }).click();
  await expect(page.getByText(/Auto-detect proposes/i)).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole('button', { name: 'Approve' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Dismiss' })).toBeVisible();
});
