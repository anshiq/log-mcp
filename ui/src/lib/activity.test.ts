import { describe, expect, it } from 'vitest';
import { dayLabel, dedupeEvents, eventKind, eventTone, humanizeType } from './activity';
import type { DaemonEvent } from './api/types';

function ev(over: Partial<DaemonEvent>): DaemonEvent {
  return { id: 1, ts: 1000, type: 'process.started', processId: 'p', instanceId: 'i', workspaceId: '', projectId: '', sessionId: '', payload: {}, cursor: '1', ...over };
}

describe('activity', () => {
  it('classifies families and tones', () => {
    expect(eventKind('process.failed').tone).toBe('err');
    expect(eventKind('logs.alert').family).toBe('logs');
    expect(eventKind('config.updated').family).toBe('config');
    expect(eventTone(ev({ type: 'process.exited', payload: { exit_code: 3 } }))).toBe('warn');
    expect(eventTone(ev({ type: 'process.exited', payload: { exit_code: 0 } }))).toBe('neutral');
  });

  it('humanizes unknown types', () => {
    expect(humanizeType('config.layer_saved')).toBe('Layer saved');
  });

  it('dedupes duplicate rows preferring the one with a workspace', () => {
    const out = dedupeEvents([ev({ id: 1 }), ev({ id: 2, workspaceId: 'ws' }), ev({ id: 3, ts: 2000 })]);
    expect(out).toHaveLength(2);
    expect(out.find((e) => e.ts === 1000)?.workspaceId).toBe('ws');
  });

  it('labels days', () => {
    const now = new Date(2026, 8, 29, 12).getTime();
    expect(dayLabel(now - 3600000, now)).toBe('Today');
    expect(dayLabel(now - 86400000, now)).toBe('Yesterday');
  });
});
