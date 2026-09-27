import { test, expect } from '@playwright/test';
import { e2eInfo, loginUrl } from './state';

test('auto-login via token fragment lands on the processes page', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await expect(page.getByRole('button', { name: 'Start process' })).toBeVisible();
  await expect(page.locator('input[aria-label="Bearer token"]')).toHaveCount(0);
});

test('rejects a bad token and shows the login screen', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('input[aria-label="Bearer token"]')).toBeVisible();
  await page.locator('input[aria-label="Bearer token"]').fill('not-the-real-token');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText(/unauthorized/i)).toBeVisible();
});

test('start a process from the dialog and see it appear live', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));

  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open' }).click();
  await expect(page.getByText(/^ws$/)).toBeVisible({ timeout: 10000 });

  await page.getByRole('button', { name: 'Start process' }).click();
  await page.getByRole('button', { name: 'Command' }).click();
  const commandInput = page.locator('label').filter({ hasText: 'Command' }).locator('input');
  await commandInput.fill('sh -c "echo hello-e2e; sleep 30"');
  await page.getByRole('button', { name: 'Start', exact: true }).click();

  await expect(page.locator('.mono.link', { hasText: 'sh -c echo hello-e2e; sleep 30' })).toBeVisible({ timeout: 10000 });
  await expect(page.getByText('Running 1', { exact: false })).toBeVisible();
});

test('stop the running process and see its status change live', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  const row = page.locator('tr', { hasText: 'echo hello-e2e' });
  await expect(row).toBeVisible({ timeout: 10000 });
  await row.getByTitle('Stop').click();
  await expect(row.locator('.dot.off')).toBeVisible({ timeout: 10000 });
});

test('command palette opens with Ctrl+K and navigates', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await page.keyboard.press('Control+k');
  const paletteInput = page.getByPlaceholder('Search pages, processes…');
  await expect(paletteInput).toBeVisible();
  await paletteInput.fill('Settings');
  await page.keyboard.press('Enter');
  await expect(page.getByText('Daemon settings')).toBeVisible();
});

test('config edit, plan and apply round-trips through the daemon', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open' }).click();

  await page.getByRole('button', { name: 'Config', exact: true }).click();
  const editor = page.locator('.monaco-editor').first();
  await expect(editor).toBeVisible({ timeout: 10000 });
  await editor.click();
  await page.keyboard.press('Control+a');
  await page.keyboard.type('apps:\n  e2eapp:\n    command: ["sleep", "5"]\n');

  await page.getByRole('button', { name: 'Plan' }).click();
  await expect(page.locator('.changes strong', { hasText: 'e2eapp' })).toBeVisible({ timeout: 10000 });
  await page.getByRole('button', { name: 'Apply' }).click();
  await expect(page.getByText('Applied · revision')).toBeVisible({ timeout: 10000 });
});
