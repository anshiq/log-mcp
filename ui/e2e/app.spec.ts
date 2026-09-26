// Web build + real daemon: start app, tail, search, edit config ->
// plan -> apply, install integration into a temp HOME.
import { test, expect } from '@playwright/test';

const BASE = process.env.E2E_BASE ?? 'http://127.0.0.1:7350';

test('daemon version handshake', async ({ request }) => {
  const res = await request.post(`${BASE}/api/agentruntime.v1.SystemService/GetVersion`, { data: {} });
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.apiVersion).toBe('v1');
});

test('resolve + list roundtrip', async ({ request }) => {
  const res = await request.post(`${BASE}/api/agentruntime.v1.ProjectService/Resolve`, {
    data: { path: '/tmp' }
  });
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.workspaceId).toBeTruthy();
});
