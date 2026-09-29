<script lang="ts">
  let { width = '100%', height = '16px', radius = '', circle = false }: { width?: string; height?: string; radius?: string; circle?: boolean } = $props();
</script>

<div class="sk" class:circle style={`width:${width};height:${height}${radius ? `;border-radius:${radius}` : ''}`} aria-hidden="true"></div>

<style>
  .sk {
    background: linear-gradient(90deg, var(--bg-3) 0%, var(--bg-hover) 45%, var(--bg-3) 90%);
    background-size: 220% 100%;
    border-radius: var(--radius-sm);
    animation: shimmer 1.5s ease-in-out infinite;
    flex: none;
    max-width: 100%;
  }
  .sk.circle {
    border-radius: 50%;
  }
  @keyframes shimmer {
    from {
      background-position: 120% 0;
    }
    to {
      background-position: -100% 0;
    }
  }
</style>
