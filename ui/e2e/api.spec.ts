import { test, expect } from '@playwright/test';
import { e2eInfo, baseUrl } from './state';

test('daemon version handshake', async ({ request }) => {
  const info = e2eInfo();
  const res = await request.post(`${baseUrl(info)}/api/agentruntime.v1.SystemService/GetVersion`, {
    headers: { Authorization: `Bearer ${info.token}` },
    data: {}
  });
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.apiVersion).toBe('v1');
});

test('resolve + list roundtrip', async ({ request }) => {
  const info = e2eInfo();
  const res = await request.post(`${baseUrl(info)}/api/agentruntime.v1.ProjectService/Resolve`, {
    headers: { Authorization: `Bearer ${info.token}` },
    data: { path: info.workspace }
  });
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.workspaceId).toBeTruthy();
});

test('unauthenticated request is rejected', async ({ request }) => {
  const info = e2eInfo();
  const res = await request.post(`${baseUrl(info)}/api/agentruntime.v1.SystemService/GetVersion`, { data: {} });
  expect(res.status()).toBe(401);
});
