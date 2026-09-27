import { readFileSync } from 'node:fs';

export interface E2EInfo {
  token: string;
  port: string;
  workspace: string;
}

export function e2eInfo(): E2EInfo {
  const file = process.env.E2E_STATE_FILE;
  if (!file) throw new Error('E2E_STATE_FILE not set; did globalSetup run?');
  return JSON.parse(readFileSync(file, 'utf-8'));
}

export function baseUrl(info: E2EInfo): string {
  return `http://127.0.0.1:${info.port}`;
}

export function loginUrl(info: E2EInfo): string {
  return `${baseUrl(info)}/#token=${encodeURIComponent(info.token)}`;
}
