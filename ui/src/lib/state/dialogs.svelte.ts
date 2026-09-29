export interface ConfirmOptions {
  title?: string;
  confirmLabel?: string;
  danger?: boolean;
}

interface ConfirmState extends ConfirmOptions {
  message: string;
  resolve: (v: boolean) => void;
}

class Dialogs {
  confirmState = $state<ConfirmState | null>(null);

  confirm(message: string, opts: ConfirmOptions = {}): Promise<boolean> {
    return new Promise((resolve) => {
      this.confirmState?.resolve(false);
      this.confirmState = { message, resolve, ...opts };
    });
  }

  resolveConfirm(v: boolean) {
    const current = this.confirmState;
    this.confirmState = null;
    current?.resolve(v);
  }
}

export const dialogs = new Dialogs();
