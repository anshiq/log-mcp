import * as monaco from 'monaco-editor/esm/vs/editor/editor.api';
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution';
import { configureMonacoYaml } from 'monaco-yaml';

let configured = false;

export function ensureMonacoConfigured(schema: unknown): void {
  if (configured) return;
  configured = true;

  self.MonacoEnvironment = {
    getWorker(_moduleId: string, label: string) {
      if (label === 'yaml') {
        return new Worker(new URL('monaco-yaml/yaml.worker.js', import.meta.url), { type: 'module' });
      }
      return new Worker(new URL('monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url), { type: 'module' });
    }
  };

  configureMonacoYaml(monaco, {
    enableSchemaRequest: false,
    schemas: [
      {
        uri: 'inmemory://agent-runtime-v3-schema.json',
        fileMatch: ['*'],
        schema: schema as Record<string, unknown>
      }
    ]
  });
}

export { monaco };
