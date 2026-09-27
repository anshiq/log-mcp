class Dialogs {
  confirmState = $state<{ message: string; resolve: (v: boolean) => void } | null>(null);

  confirm(message: string): Promise<boolean> {
    return new Promise((resolve) => {
      this.confirmState = { message, resolve };
    });
  }

  resolveConfirm(v: boolean) {
    this.confirmState?.resolve(v);
    this.confirmState = null;
  }
}

export const dialogs = new Dialogs();
