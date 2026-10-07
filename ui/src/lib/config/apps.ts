import type { Process } from '../api/types';

export interface ConfigApp {
  name: string;
  type: string;
  command: string[];
  workdir: string;
  dependsOn: string[];
  autostart: boolean;
  lifetime: string;
  reload: string;
  pty: boolean;
  readiness: string[];
  healthHttp: string;
  healthTcp: string;
  restartPolicy: string;
  ports: { name: string; port: number }[];
  envCount: number;
  provenance: string;
  auto: boolean;
}

type Rec = Record<string, unknown>;

function pick(o: Rec, ...keys: string[]): unknown {
  for (const k of keys) if (o[k] !== undefined && o[k] !== null) return o[k];
  return undefined;
}

function strs(v: unknown): string[] {
  return Array.isArray(v) ? v.map((x) => String(x)) : [];
}

function rec(v: unknown): Rec {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Rec) : {};
}

export function toConfigApp(name: string, raw: unknown, provenance = '', auto = false): ConfigApp {
  const o = rec(raw);
  const health = rec(pick(o, 'HealthCheck', 'health_check'));
  const restart = rec(pick(o, 'Restart', 'restart'));
  const portsRaw = rec(pick(o, 'Ports', 'ports'));
  const ports = Object.entries(portsRaw)
    .map(([n, p]) => ({ name: n, port: Number(pick(rec(p), 'Default', 'default') ?? 0) }))
    .filter((p) => p.port > 0);
  return {
    name,
    type: String(pick(o, 'Type', 'type') ?? ''),
    command: strs(pick(o, 'Command', 'command')),
    workdir: String(pick(o, 'WorkDir', 'workdir') ?? ''),
    dependsOn: strs(pick(o, 'DependsOn', 'depends_on')),
    autostart: pick(o, 'Autostart', 'autostart') === true,
    lifetime: String(pick(o, 'Lifetime', 'lifetime') ?? 'persistent'),
    reload: String(pick(o, 'Reload', 'reload') ?? 'manual'),
    pty: pick(o, 'Pty', 'pty') === true,
    readiness: strs(pick(o, 'Readiness', 'readiness')),
    healthHttp: String(pick(health, 'HTTP', 'http') ?? ''),
    healthTcp: String(pick(health, 'TCP', 'tcp') ?? ''),
    restartPolicy: String(pick(restart, 'Policy', 'policy') ?? ''),
    ports,
    envCount: strs(pick(o, 'Env', 'env')).length,
    provenance,
    auto
  };
}

export function toConfigApps(apps: unknown, provenance: Record<string, unknown> = {}, autoApps: unknown = []): ConfigApp[] {
  const autoSet = new Set(Array.isArray(autoApps) ? autoApps.map(String) : []);
  return Object.entries(rec(apps))
    .map(([name, a]) => toConfigApp(name, a, String(provenance[`apps.${name}`] ?? ''), autoSet.has(name)))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function quoteArg(arg: string): string {
  if (arg === '') return "''";
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(arg)) return arg;
  return `'${arg.replace(/'/g, `'\\''`)}'`;
}

export function commandLine(command: string[]): string {
  return command.map(quoteArg).join(' ');
}

const LIVE = new Set(['running', 'ready', 'starting']);

export function isLive(status: string): boolean {
  return LIVE.has(status);
}

export function matchProcesses(app: ConfigApp, procs: Process[]): Process[] {
  const byName = procs.filter((p) => p.app === app.name);
  if (byName.length > 0) return byName;
  if (app.command.length === 0) return [];
  return procs.filter((p) => {
    if (p.app) return false;
    if (p.command !== app.command[0]) return false;
    const args = p.args ?? [];
    return args.length === app.command.length - 1 && args.every((a, i) => a === app.command[i + 1]);
  });
}

export function primaryProcess(matches: Process[]): Process | null {
  return matches.find((p) => isLive(p.status)) ?? matches[0] ?? null;
}
