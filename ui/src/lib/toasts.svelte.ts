export interface Toast {
  id: number;
  message: string;
  tone: 'info' | 'ok' | 'err';
  action?: { label: string; onClick: () => void };
}

let nextId = 1;

class Toasts {
  items = $state<Toast[]>([]);

  push(message: string, tone: Toast['tone'] = 'info', action?: Toast['action']): number {
    const id = nextId++;
    this.items.push({ id, message, tone, action });
    if (tone !== 'err') {
      setTimeout(() => this.dismiss(id), 5000);
    }
    return id;
  }

  ok(message: string, action?: Toast['action']) {
    return this.push(message, 'ok', action);
  }

  err(message: string, action?: Toast['action']) {
    return this.push(message, 'err', action);
  }

  info(message: string, action?: Toast['action']) {
    return this.push(message, 'info', action);
  }

  dismiss(id: number) {
    this.items = this.items.filter((t) => t.id !== id);
  }
}

export const toasts = new Toasts();

export function toastError(err: unknown, fallback = 'Something went wrong'): void {
  const message = err instanceof Error ? err.message : String(err ?? fallback);
  toasts.err(message);
}
