<script lang="ts">
  let { size = 16, label = '' }: { size?: number; label?: string } = $props();
</script>

<svg
  class="spinner"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  role={label ? 'status' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
>
  <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.22" />
  <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
</svg>

<style>
  .spinner {
    animation: spin 0.75s linear infinite;
    flex: none;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 2.4s !important;
      animation-iteration-count: infinite !important;
    }
  }
</style>
