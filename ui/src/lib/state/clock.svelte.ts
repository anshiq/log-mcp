class Clock {
  now = $state(Date.now());
  private refs = 0;
  private timer: ReturnType<typeof setInterval> | null = null;

  retain(): () => void {
    this.refs++;
    if (!this.timer) {
      this.now = Date.now();
      this.timer = setInterval(() => (this.now = Date.now()), 5000);
    }
    return () => {
      this.refs--;
      if (this.refs <= 0 && this.timer) {
        clearInterval(this.timer);
        this.timer = null;
        this.refs = 0;
      }
    };
  }
}

export const clock = new Clock();
