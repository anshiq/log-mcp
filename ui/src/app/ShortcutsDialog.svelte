<script lang="ts">
  import Keyboard from '@lucide/svelte/icons/keyboard';
  import Dialog from '../lib/ui/Dialog.svelte';
  import Kbd from '../lib/ui/Kbd.svelte';

  let { open = $bindable(false) }: { open?: boolean } = $props();

  const groups: { title: string; rows: { keys: string; label: string }[] }[] = [
    {
      title: 'Navigation',
      rows: [
        { keys: 'Mod+K', label: 'Open command palette' },
        { keys: 'g o', label: 'Go to Overview' },
        { keys: 'g p', label: 'Go to Processes' },
        { keys: 'g l', label: 'Go to Logs' },
        { keys: 'g a', label: 'Go to Apps' },
        { keys: 'g c', label: 'Go to Config' },
        { keys: 'g e', label: 'Go to Events' },
        { keys: 'g s', label: 'Go to Sessions' },
        { keys: 'g u', label: 'Go to Audit' },
        { keys: 'g j', label: 'Go to Projects' },
        { keys: 'g i', label: 'Go to Integrations' },
        { keys: 'g t', label: 'Go to Settings' }
      ]
    },
    {
      title: 'Actions',
      rows: [
        { keys: '/', label: 'Focus the search or filter field' },
        { keys: '?', label: 'Show this keyboard shortcuts dialog' },
        { keys: 'Esc', label: 'Close dialog, palette or panel' }
      ]
    },
    {
      title: 'Logs & processes',
      rows: [
        { keys: '[', label: 'Select previous process' },
        { keys: ']', label: 'Select next process' },
        { keys: 'Esc', label: 'Close process details' }
      ]
    }
  ];
</script>

{#if open}
  <Dialog title="Keyboard shortcuts" description="Press g, then a letter, to jump between pages." icon={Keyboard} width={560} flush onClose={() => (open = false)}>
    <div class="scroll">
      {#each groups as g (g.title)}
        <section>
          <h3>{g.title}</h3>
          <table>
            <tbody>
              {#each g.rows as r (r.keys + r.label)}
                <tr>
                  <td class="what">{r.label}</td>
                  <td class="keys"><Kbd keys={r.keys} /></td>
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
      {/each}
    </div>
  </Dialog>
{/if}

<style>
  .scroll {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    padding: var(--space-3) var(--space-6) var(--space-6);
  }
  h3 {
    margin-bottom: var(--space-2);
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
  }
  td {
    padding: 7px 0;
    border-bottom: 1px solid var(--border);
  }
  tr:last-child td {
    border-bottom: none;
  }
  .what {
    color: var(--text-1);
  }
  .keys {
    text-align: right;
    white-space: nowrap;
  }
</style>
