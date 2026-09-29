import type { Component } from 'svelte';

export interface MenuItem {
  id: string;
  label?: string;
  icon?: Component<{ size?: number }>;
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  separator?: boolean;
  heading?: string;
  onSelect?: () => void;
}
