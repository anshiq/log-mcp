import { ConfigService, isApiError, openStream } from '../api';
import type { PlanResult, Revision } from '../api/types';
import { toasts, toastError } from '../toasts.svelte';
import { toConfigApps, type ConfigApp } from './apps';

export type LayerName = 'project' | 'workspace';

export interface LayerInfo {
  name: LayerName;
  path: string;
  exists: boolean;
  writable: boolean;
  trusted: boolean;
  sha256: string;
}

export interface ValidationIssue {
  line: number;
  column: number;
  path: string;
  message: string;
}

export interface PlanChange {
  app: string;
  kind: string;
  fields: string[];
  affected: string[];
}

export interface PlanState {
  layer: LayerName;
  forText: string;
  changes: PlanChange[];
  stale: boolean;
  latestRevision: number;
  diff: string;
  errors: ValidationIssue[];
  supported: boolean;
  unaffected: string[];
}

export interface ExternalChange {
  revision: number;
  source: string;
  message: string;
  layer: string;
  invalid: boolean;
}

export interface ChoiceOption {
  id: string;
  label: string;
  tone?: 'primary' | 'danger' | 'default';
}

export interface ChoiceRequest {
  title: string;
  message: string;
  details?: string[];
  options: ChoiceOption[];
  resolve: (id: string | null) => void;
}

const LAYERS: LayerName[] = ['project', 'workspace'];

function emptyTexts(): Record<LayerName, string> {
  return { project: '', workspace: '' };
}

function issues(raw: unknown): ValidationIssue[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((e) => {
    if (typeof e === 'string') return { line: 1, column: 1, path: '', message: e };
    const o = e as Record<string, unknown>;
    return {
      line: Math.max(1, Number(o['line'] ?? 1)),
      column: Math.max(1, Number(o['column'] ?? 1)),
      path: String(o['path'] ?? ''),
      message: String(o['message'] ?? JSON.stringify(e))
    };
  });
}

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

function changesOf(raw: unknown): PlanChange[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((c) => {
    const o = asRecord(c);
    const affected = o['affected_process_ids'] ?? o['affectedProcIds'] ?? o['affectedProcessIds'];
    return {
      app: String(o['app'] ?? ''),
      kind: String(o['kind'] ?? ''),
      fields: Array.isArray(o['fields']) ? (o['fields'] as unknown[]).map(String) : [],
      affected: Array.isArray(affected) ? (affected as unknown[]).map(String) : []
    };
  });
}

export class ConfigSession {
  workspaceId = $state('');
  projectId = $state('');
  loading = $state(false);
  loaded = $state(false);
  loadError = $state('');
  layers = $state<LayerInfo[]>([]);
  warnings = $state<string[]>([]);
  apps = $state<ConfigApp[]>([]);
  revision = $state(0);
  layer = $state<LayerName>('project');
  saved = $state<Record<LayerName, string>>(emptyTexts());
  drafts = $state<Record<LayerName, string>>(emptyTexts());
  version = $state(0);
  revisions = $state<Revision[]>([]);
  revisionsLoading = $state(false);
  errors = $state<ValidationIssue[]>([]);
  validationWarnings = $state<string[]>([]);
  validating = $state(false);
  validatedText = $state<string | null>(null);
  plan = $state<PlanState | null>(null);
  planning = $state(false);
  applying = $state(false);
  stale = $state<{ base: number; latest: number } | null>(null);
  external = $state<ExternalChange | null>(null);
  lastApplied = $state<{ revision: number; at: number; restarted: number } | null>(null);
  choice = $state<ChoiceRequest | null>(null);
  drawerOpen = $state(false);
  message = $state('');

  provenance = $state<Record<string, string>>({});

  originOf(app: string): string {
    return this.provenance[`apps.${app}`] ?? '';
  }

  private timer: ReturnType<typeof setTimeout> | null = null;
  private watcher: { close(): void } | null = null;
  private own = new Set<number>();
  private known = 0;
  private loadToken = 0;

  get text(): string {
    return this.drafts[this.layer];
  }

  get layerInfo(): LayerInfo | null {
    return this.layers.find((l) => l.name === this.layer) ?? null;
  }

  get writable(): boolean {
    return this.layerInfo?.writable ?? true;
  }

  isDirty(layer: LayerName): boolean {
    return this.drafts[layer] !== this.saved[layer];
  }

  get dirty(): boolean {
    return this.isDirty(this.layer);
  }

  get anyDirty(): boolean {
    return LAYERS.some((l) => this.isDirty(l));
  }

  get dirtyLayers(): LayerName[] {
    return LAYERS.filter((l) => this.isDirty(l));
  }

  get empty(): boolean {
    return this.text.trim() === '';
  }

  get valid(): boolean {
    return !this.empty && this.validatedText === this.text && this.errors.length === 0;
  }

  get invalid(): boolean {
    return this.errors.length > 0;
  }

  get baseRevision(): number {
    if (this.layer === 'project') return this.revision;
    return this.layerRevision(this.layer);
  }

  layerRevision(layer: string): number {
    let latest = 0;
    for (const r of this.revisions) if (r.layer === layer && r.id > latest) latest = r.id;
    return latest;
  }

  latestRevisionFor(layer: string): Revision | null {
    let best: Revision | null = null;
    for (const r of this.revisions) if (r.layer === layer && (!best || r.id > best.id)) best = r;
    return best;
  }

  get planCurrent(): PlanState | null {
    if (!this.plan) return null;
    return this.plan.forText === this.text && this.plan.layer === this.layer ? this.plan : null;
  }

  get restartCount(): number {
    const p = this.planCurrent;
    if (!p) return 0;
    let n = 0;
    for (const c of p.changes) if (c.kind === 'restart-required') n += c.affected.length;
    return n;
  }

  async load(workspaceId: string, opts: { keepLayer?: boolean } = {}): Promise<void> {
    if (!workspaceId) return;
    const token = ++this.loadToken;
    this.loading = true;
    this.loadError = '';
    try {
      const cfg = await ConfigService.get(workspaceId);
      if (token !== this.loadToken) return;
      const raw = (cfg.raw ?? {}) as Record<string, string>;
      const rawLayers = (cfg.layers ?? []) as unknown as Record<string, unknown>[];
      if (workspaceId !== this.workspaceId) {
        this.known = 0;
        this.own.clear();
        this.lastApplied = null;
      }
      this.workspaceId = workspaceId;
      this.projectId = String(cfg.projectId ?? '');
      this.revision = Number(cfg.revision ?? 0);
      this.warnings = ((cfg as unknown as { warnings?: string[] | null }).warnings ?? []).map(String);
      this.layers = LAYERS.map((name) => {
        const l = rawLayers.find((x) => x['name'] === name) ?? {};
        return {
          name,
          path: String(l['path'] ?? ''),
          exists: l['exists'] === true,
          writable: l['writable'] === true,
          trusted: l['trusted'] !== false,
          sha256: String(l['sha256'] ?? '')
        };
      });
      const prov = (cfg as unknown as { provenance?: Record<string, unknown> }).provenance ?? {};
      this.provenance = Object.fromEntries(Object.entries(prov).map(([k, v]) => [k, String(v)]));
      this.apps = toConfigApps(cfg.apps, prov);
      const next = emptyTexts();
      for (const l of LAYERS) next[l] = typeof raw[l] === 'string' ? (raw[l] as string) : '';
      this.saved = { ...next };
      this.drafts = { ...next };
      if (!opts.keepLayer) this.layer = 'project';
      this.plan = null;
      this.stale = null;
      this.external = null;
      this.errors = [];
      this.validatedText = null;
      this.version++;
      this.loaded = true;
      await this.loadRevisions();
      if (token !== this.loadToken) return;
      this.known = Math.max(this.known, this.revision, ...this.revisions.map((r) => r.id));
      this.startWatch();
      void this.validateNow();
    } catch (err) {
      if (token !== this.loadToken) return;
      this.loadError = err instanceof Error ? err.message : String(err);
    } finally {
      if (token === this.loadToken) this.loading = false;
    }
  }

  async loadRevisions(): Promise<void> {
    if (!this.projectId) return;
    this.revisionsLoading = true;
    try {
      this.revisions = await ConfigService.revisions(this.projectId, 100);
    } catch (err) {
      toastError(err);
    } finally {
      this.revisionsLoading = false;
    }
  }

  reset() {
    this.stopWatch();
    this.loadToken++;
    this.workspaceId = '';
    this.projectId = '';
    this.loaded = false;
    this.loading = false;
    this.loadError = '';
    this.layers = [];
    this.apps = [];
    this.revisions = [];
    this.saved = emptyTexts();
    this.drafts = emptyTexts();
    this.plan = null;
    this.stale = null;
    this.external = null;
    this.errors = [];
    this.validatedText = null;
    this.drawerOpen = false;
    this.lastApplied = null;
    this.known = 0;
    this.version++;
  }

  setLayer(layer: LayerName) {
    if (layer === this.layer) return;
    this.layer = layer;
    this.errors = [];
    this.validatedText = null;
    this.plan = null;
    this.stale = null;
    this.drawerOpen = false;
    this.version++;
    void this.validateNow();
  }

  setText(value: string) {
    if (this.drafts[this.layer] === value) return;
    this.drafts[this.layer] = value;
    this.scheduleValidate();
  }

  replaceText(value: string) {
    this.drafts[this.layer] = value;
    this.version++;
    this.scheduleValidate();
  }

  discard() {
    this.drafts[this.layer] = this.saved[this.layer];
    this.plan = null;
    this.stale = null;
    this.version++;
    void this.validateNow();
  }

  private scheduleValidate() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.validateNow(), 300);
  }

  async validateNow(): Promise<boolean> {
    if (this.timer) clearTimeout(this.timer);
    const text = this.text;
    if (text.trim() === '') {
      this.errors = [];
      this.validationWarnings = [];
      this.validatedText = text;
      return false;
    }
    this.validating = true;
    try {
      const res = await ConfigService.validate(text);
      if (text !== this.text) return false;
      this.errors = res.valid ? [] : issues(res.errors);
      this.validationWarnings = (res.warnings ?? []).map(String);
      this.validatedText = text;
      return res.valid;
    } catch (err) {
      toastError(err);
      return false;
    } finally {
      this.validating = false;
    }
  }

  async runPlan(): Promise<PlanState | null> {
    if (this.planning) return null;
    const text = this.text;
    const layer = this.layer;
    this.planning = true;
    try {
      if (layer !== 'project') {
        this.plan = { layer, forText: text, changes: [], stale: false, latestRevision: this.baseRevision, diff: '', errors: [], supported: false, unaffected: [] };
        return this.plan;
      }
      const res = (await ConfigService.plan(this.workspaceId, text, this.revision)) as PlanResult & { errors?: unknown };
      const errors = issues(res.errors);
      const all = changesOf(res.changes);
      const unaffected = all.filter((c) => c.kind === 'removed' && this.originOf(c.app) !== '' && this.originOf(c.app) !== 'project').map((c) => c.app);
      const state: PlanState = {
        layer,
        forText: text,
        changes: all.filter((c) => !unaffected.includes(c.app)),
        unaffected,
        stale: res.stale === true,
        latestRevision: Number(res.latestRevision ?? 0),
        diff: String(res.diff ?? ''),
        errors,
        supported: true
      };
      this.plan = state;
      if (errors.length > 0) this.errors = errors;
      if (state.stale) this.stale = { base: this.revision, latest: state.latestRevision };
      return state;
    } catch (err) {
      toastError(err);
      return null;
    } finally {
      this.planning = false;
    }
  }

  async preview() {
    if (this.empty) return;
    this.drawerOpen = true;
    await this.runPlan();
  }

  choose(req: Omit<ChoiceRequest, 'resolve'>): Promise<string | null> {
    return new Promise((resolve) => {
      this.choice = { ...req, resolve };
    });
  }

  answer(id: string | null) {
    const c = this.choice;
    this.choice = null;
    c?.resolve(id);
  }

  async confirm(title: string, message: string, label: string, tone: 'primary' | 'danger' = 'primary'): Promise<boolean> {
    const id = await this.choose({ title, message, options: [{ id: 'yes', label, tone }] });
    return id === 'yes';
  }

  async apply(): Promise<boolean> {
    if (this.applying || !this.writable || this.empty) return false;
    const ok = await this.validateNow();
    if (!ok) {
      toasts.err(this.errors.length > 0 ? `Cannot apply: ${this.errors.length} validation error${this.errors.length === 1 ? '' : 's'}` : 'Cannot apply an empty config');
      return false;
    }
    let plan = this.planCurrent;
    if (!plan) plan = await this.runPlan();
    if (!plan) return false;
    if (plan.errors.length > 0) return false;
    if (plan.stale) {
      this.drawerOpen = true;
      return false;
    }
    let restart = false;
    const restarts = plan.changes.filter((c) => c.kind === 'restart-required' && c.affected.length > 0);
    if (restarts.length > 0) {
      const count = restarts.reduce((n, c) => n + c.affected.length, 0);
      const choice = await this.choose({
        title: 'Restart affected processes?',
        message: `${count} running process${count === 1 ? '' : 'es'} use settings that changed and need a restart to pick them up.`,
        details: restarts.map((c) => `${c.app}: ${c.fields.join(', ')} (${c.affected.length} running)`),
        options: [
          { id: 'apply', label: 'Apply only' },
          { id: 'restart', label: 'Apply and restart', tone: 'primary' }
        ]
      });
      if (!choice) return false;
      restart = choice === 'restart';
    }
    return this.commit(restart);
  }

  private async commit(restart: boolean): Promise<boolean> {
    this.applying = true;
    const text = this.text;
    const layer = this.layer;
    try {
      const res = await ConfigService.apply({
        projectId: this.projectId,
        workspaceId: this.workspaceId,
        layer,
        yaml: text,
        baseRevision: this.baseRevision,
        message: this.message.trim() || 'edited from gui',
        restartAffected: restart
      });
      if (res.applied) {
        const rev = Number(res.revision ?? 0);
        this.own.add(rev);
        this.known = Math.max(this.known, rev);
        if (layer === 'project') this.revision = rev;
        this.saved[layer] = text;
        this.plan = null;
        this.stale = null;
        this.external = null;
        this.message = '';
        this.drawerOpen = false;
        this.lastApplied = { revision: rev, at: Date.now(), restarted: res.restarted?.length ?? 0 };
        toasts.ok(`Applied · revision ${rev}`);
        void this.loadRevisions();
        void this.refreshApps();
        return true;
      }
      this.errors = issues(res.errors);
      this.validatedText = text;
      return false;
    } catch (err) {
      if (isApiError(err) && (err.code === 'stale_revision' || /stale_revision/.test(err.message))) {
        const m = /latest (\d+)/.exec(err.message);
        this.stale = { base: this.baseRevision, latest: m ? Number(m[1]) : this.baseRevision };
        this.drawerOpen = true;
      } else {
        toastError(err);
      }
      return false;
    } finally {
      this.applying = false;
    }
  }

  async refreshApps() {
    if (!this.workspaceId) return;
    try {
      const cfg = await ConfigService.get(this.workspaceId);
      const prov = (cfg as unknown as { provenance?: Record<string, unknown> }).provenance ?? {};
      this.provenance = Object.fromEntries(Object.entries(prov).map(([k, v]) => [k, String(v)]));
      this.apps = toConfigApps(cfg.apps, prov);
    } catch {
      return;
    }
  }

  insertStarter() {
    this.replaceText('version: 3\napps:\n  app:\n    command:\n      - sh\n      - -c\n      - echo hello; sleep 60\n');
  }

  async reloadLatest(): Promise<void> {
    if (this.anyDirty) {
      const ok = await this.confirm('Discard unsaved changes?', 'Reloading pulls the latest saved config from disk and drops the edits you have not applied.', 'Discard and reload', 'danger');
      if (!ok) return;
    }
    await this.load(this.workspaceId, { keepLayer: true });
  }

  startWatch() {
    this.stopWatch();
    if (!this.projectId) return;
    const projectId = this.projectId;
    this.watcher = openStream(
      'ConfigService',
      'WatchConfig',
      { projectId, since: this.known },
      {
        resume: () => ({ projectId, since: this.known }),
        onMessage: (msg) => {
          if (msg.kind === 'revision') {
            const rev = asRecord(msg['revision']);
            const id = Number(rev['id'] ?? 0);
            if (id <= this.known || this.own.has(id)) return;
            const info: ExternalChange = {
              revision: id,
              source: String(rev['source'] ?? ''),
              message: String(rev['message'] ?? ''),
              layer: String(rev['layer'] ?? ''),
              invalid: rev['valid'] === false
            };
            setTimeout(() => {
              if (this.own.has(id) || id <= this.known) return;
              this.known = Math.max(this.known, id);
              this.external = info;
              void this.loadRevisions();
              void this.refreshApps();
            }, 500);
          }
        }
      }
    );
  }

  stopWatch() {
    this.watcher?.close();
    this.watcher = null;
  }

  dismissExternal() {
    this.external = null;
  }

  async rollback(rev: Revision): Promise<boolean> {
    const dirty = this.isDirty('project');
    const ok = await this.confirm(
      `Roll back to revision ${rev.id}?`,
      dirty
        ? 'This writes that revision as a new revision and replaces the project layer. Your unsaved project-layer edits will be lost.'
        : 'This writes that revision as a new revision and replaces the project layer. Earlier revisions stay in history.',
      'Roll back',
      'danger'
    );
    if (!ok) return false;
    try {
      const res = await ConfigService.rollback(this.projectId, rev.id);
      const id = Number(res.revision ?? 0);
      if (id) {
        this.own.add(id);
        this.known = Math.max(this.known, id);
      }
      toasts.ok(`Rolled back to revision ${rev.id}${id ? ` · new revision ${id}` : ''}`);
      await this.load(this.workspaceId, { keepLayer: true });
      return true;
    } catch (err) {
      toastError(err);
      return false;
    }
  }

  destroy() {
    this.stopWatch();
    if (this.timer) clearTimeout(this.timer);
  }
}
