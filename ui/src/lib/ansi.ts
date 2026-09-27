import Anser from 'anser';

export function ansiToHtml(text: string): string {
  return Anser.ansiToHtml(Anser.escapeForHtml(text), { use_classes: false });
}

export function stripAnsi(text: string): string {
  return text.replace(/\u001b\[[0-9;]*m/g, '');
}
